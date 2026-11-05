package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"libread/internal/store"
)

// fakeGoogle はトークンエンドポイントの代わりに、email の ID トークンを返す。
func fakeGoogle(t *testing.T, email string) func(*Config) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := json.Marshal(map[string]any{
			"iss":            "https://accounts.google.com",
			"aud":            "client-123",
			"exp":            testNow.Add(time.Hour).Unix(),
			"email":          email,
			"email_verified": true,
		})
		enc := base64.RawURLEncoding
		tok := enc.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + enc.EncodeToString(claims) + ".c2ln"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id_token": tok})
	}))
	t.Cleanup(srv.Close)
	return func(c *Config) {
		c.Auth.TokenURL = srv.URL + "/token"
		c.Auth.HTTP = srv.Client()
	}
}

// login は /auth/login を呼び、libread_login Cookie と Google へ渡した state を返す。
func login(t *testing.T, s *testServer, ret string) (*http.Cookie, string) {
	t.Helper()
	rec := s.do(t, http.MethodGet, "/auth/login?return="+url.QueryEscape(ret), "")
	if rec.Code != http.StatusFound {
		t.Fatalf("/auth/login: status = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	c := cookieNamed(rec, loginCookieName)
	if c == nil {
		t.Fatal("libread_login が設定されていない")
	}
	return c, loc.Query().Get("state")
}

func TestLoginRedirectsToGoogle(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/auth/login?return=/books", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != s.cfg.Auth.AuthURL {
		t.Errorf("Location = %s", loc)
	}
	q := loc.Query()
	if q.Get("state") == "" || q.Get("redirect_uri") != testOrigin+"/auth/callback" || q.Get("prompt") != "select_account" {
		t.Errorf("query = %v", q)
	}
	c := cookieNamed(rec, loginCookieName)
	if c == nil {
		t.Fatal("libread_login が設定されていない")
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 600 || c.Path != "/auth/" {
		t.Errorf("libread_login = %+v, want HttpOnly・Secure・SameSite=Lax・10分・Path=/auth/", c)
	}
	if !strings.Contains(c.Value, q.Get("state")) {
		t.Errorf("libread_login に state がない: %q / %q", c.Value, q.Get("state"))
	}
}

func TestCallbackIssuesSessionForOwner(t *testing.T) {
	for ret, want := range map[string]string{"/books": "/#/books", "/libraries": "/#/libraries", "/": "/#/", "https://evil.example": "/#/"} {
		t.Run(ret, func(t *testing.T) {
			s := newTestServer(t, fakeGoogle(t, "Owner@Example.com"))
			lc, state := login(t, s, ret)

			rec := s.do(t, http.MethodGet, "/auth/callback?code=abc&state="+url.QueryEscape(state), "", withCookie(lc))
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
				t.Fatalf("status = %d, Location = %q, want 302 %s", rec.Code, rec.Header().Get("Location"), want)
			}
			sc := cookieNamed(rec, sessionCookieName)
			if sc == nil || sc.Value == "" {
				t.Fatal("libread_session が設定されていない")
			}
			if !sc.HttpOnly || !sc.Secure || sc.SameSite != http.SameSiteLaxMode || sc.Path != "/" || sc.MaxAge != 30*24*60*60 {
				t.Errorf("libread_session = %+v, want HttpOnly・Secure・SameSite=Lax・Path=/・30日", sc)
			}
			if c := cookieNamed(rec, loginCookieName); c == nil || c.MaxAge >= 0 {
				t.Errorf("libread_login が消されていない: %+v", c)
			}
			// 発行した Cookie で API を使える。
			if rec := s.do(t, http.MethodGet, "/api/session", "", withCookie(sc)); rec.Code != http.StatusOK {
				t.Errorf("/api/session: status = %d", rec.Code)
			}
		})
	}
}

func TestCallbackDenied(t *testing.T) {
	tests := map[string]struct {
		email  string
		query  func(state string) string
		cookie bool
	}{
		"本人以外":             {"someone@example.com", func(s string) string { return "code=abc&state=" + url.QueryEscape(s) }, true},
		"state の不一致":       {testOwner, func(string) string { return "code=abc&state=other" }, true},
		"state なし":         {testOwner, func(string) string { return "code=abc" }, true},
		"libread_login なし": {testOwner, func(s string) string { return "code=abc&state=" + url.QueryEscape(s) }, false},
		"Google のエラー":      {testOwner, func(s string) string { return "error=access_denied&state=" + url.QueryEscape(s) }, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t, fakeGoogle(t, tt.email))
			lc, state := login(t, s, "/books")
			var opts []reqOpt
			if tt.cookie {
				opts = append(opts, withCookie(lc))
			}
			rec := s.do(t, http.MethodGet, "/auth/callback?"+tt.query(state), "", opts...)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/#/login?error=denied" {
				t.Errorf("status = %d, Location = %q, want 302 /#/login?error=denied", rec.Code, rec.Header().Get("Location"))
			}
			if sc := cookieNamed(rec, sessionCookieName); sc != nil && sc.MaxAge >= 0 {
				t.Errorf("libread_session が発行された: %+v", sc)
			}
			if c := cookieNamed(rec, loginCookieName); c == nil || c.MaxAge >= 0 {
				t.Errorf("libread_login が消されていない: %+v", c)
			}
		})
	}
}

func TestGetSession(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/api/session", "", withCookie(s.sessionCookieAt(t, testNow.Add(-24*time.Hour))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertMatchesExample(t, "session", rec.Body.Bytes())
	var got struct {
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := "2026-12-04T17:00:00+09:00"; got.ExpiresAt != want {
		t.Errorf("expiresAt = %q, want %q", got.ExpiresAt, want)
	}
	if strings.Contains(rec.Body.String(), "@") {
		t.Errorf("応答にメールアドレスが含まれている: %s", rec.Body)
	}
}

func TestUnauthenticatedMatchesExample(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/api/session", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	assertMatchesExample(t, "error-unauthenticated", rec.Body.Bytes())
}

func TestLogout(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodPost, "/api/session/logout", "", withCookie(s.sessionCookie(t)), withOrigin(testOrigin))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if c := cookieNamed(rec, sessionCookieName); c == nil || c.MaxAge >= 0 || c.Path != "/" {
		t.Errorf("libread_session が消されていない: %+v", c)
	}
}

func TestRevokeAll(t *testing.T) {
	s := newTestServer(t)
	before := s.sessionCookie(t)
	other := s.sessionCookieAt(t, testNow.Add(-time.Hour)) // 別の端末

	rec := s.do(t, http.MethodPost, "/api/session/revoke-all", "", withCookie(before), withOrigin(testOrigin))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if c := cookieNamed(rec, sessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Errorf("libread_session が消されていない: %+v", c)
	}
	st, err := store.LoadAuthState(s.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Generation != 2 {
		t.Errorf("generation = %d, want 2", st.Generation)
	}
	for name, c := range map[string]*http.Cookie{"この端末": before, "別の端末": other} {
		if rec := s.do(t, http.MethodGet, "/api/session", "", withCookie(c)); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: 取り消した後の status = %d, want 401", name, rec.Code)
		}
	}
	// 取り消した後に発行した Cookie は使える。
	if rec := s.do(t, http.MethodGet, "/api/session", "", withCookie(s.sessionCookie(t))); rec.Code != http.StatusOK {
		t.Errorf("新しい世代の Cookie: status = %d, want 200", rec.Code)
	}
}

// 保存先（auth.json）を読めないときは、本人確認の失敗ではなく保存先の失敗として扱う（contracts/http-api.md「共通」の 500）。
func TestUnreadableAuthStateIsInternalError(t *testing.T) {
	s := newTestServer(t, fakeGoogle(t, testOwner))
	c := s.sessionCookie(t)
	lc, state := login(t, s, "/books")
	if err := os.WriteFile(filepath.Join(s.cfg.DataDir, "auth.json"), []byte(`{"version": 1,`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Cookie がなければ、保存先を読まずに一律の 401（FR-110）。
	if rec := s.do(t, http.MethodGet, "/api/books", ""); rec.Code != http.StatusUnauthorized ||
		strings.TrimSpace(rec.Body.String()) != unauthenticatedBody {
		t.Errorf("Cookie なし: %d %s, want 一律の 401", rec.Code, rec.Body)
	}

	rec := s.do(t, http.MethodGet, "/api/session", "", withCookie(c))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Errorf("Cookie あり: %d %s, want 500 internal", rec.Code, rec.Body)
	}

	rec = s.do(t, http.MethodGet, "/auth/callback?code=abc&state="+url.QueryEscape(state), "", withCookie(lc))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("コールバック: status = %d, Location = %q, want 500（本人以外とは扱わない）", rec.Code, rec.Header().Get("Location"))
	}
	if sc := cookieNamed(rec, sessionCookieName); sc != nil && sc.MaxAge >= 0 {
		t.Errorf("libread_session が発行された: %+v", sc)
	}
}
