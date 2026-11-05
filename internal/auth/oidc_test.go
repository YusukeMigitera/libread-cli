package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

var oidcNow = time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)

// fakeIDToken は署名部分が空の ID トークンを作る（トークンエンドポイントから直接受け取るので署名は検証しない）。
func fakeIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc.EncodeToString(payload) + ".c2ln"
}

func ownerClaims() map[string]any {
	return map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            "client-123",
		"exp":            oidcNow.Add(time.Hour).Unix(),
		"email":          "Owner@Example.com",
		"email_verified": true,
	}
}

// fakeTokenEndpoint は Google のトークンエンドポイントの代わり。
type fakeTokenEndpoint struct {
	mu      sync.Mutex
	status  int // 0 なら 200
	idToken string
	form    url.Values
	method  string
}

func (f *fakeTokenEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.method = r.Method
	if err := r.ParseForm(); err == nil {
		f.form = r.PostForm
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		w.Write([]byte(`{"error": "invalid_grant"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"access_token": "at", "id_token": f.idToken, "token_type": "Bearer"})
}

func newOIDC(t *testing.T, ep *fakeTokenEndpoint) Config {
	t.Helper()
	srv := httptest.NewServer(ep)
	t.Cleanup(srv.Close)
	return Config{
		ClientID:     "client-123",
		ClientSecret: "secret-456",
		PublicOrigin: "https://libread.example",
		OwnerEmail:   "owner@example.com",
		AuthURL:      "https://accounts.example/o/oauth2/v2/auth",
		TokenURL:     srv.URL + "/token",
		HTTP:         srv.Client(),
		Now:          func() time.Time { return oidcNow },
	}
}

func TestLoginURL(t *testing.T) {
	c := newOIDC(t, &fakeTokenEndpoint{})
	u, err := url.Parse(c.LoginURL("state-xyz"))
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != c.AuthURL {
		t.Errorf("URL = %s, want %s", got, c.AuthURL)
	}
	want := map[string]string{
		"response_type": "code",
		"client_id":     "client-123",
		"redirect_uri":  "https://libread.example/auth/callback",
		"scope":         "openid email",
		"state":         "state-xyz",
		"prompt":        "select_account",
	}
	q := u.Query()
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestExchangeAcceptsOwner(t *testing.T) {
	for _, iss := range []string{"https://accounts.google.com", "accounts.google.com"} {
		t.Run(iss, func(t *testing.T) {
			claims := ownerClaims()
			claims["iss"] = iss
			ep := &fakeTokenEndpoint{idToken: fakeIDToken(t, claims)}
			c := newOIDC(t, ep)
			if err := c.Exchange(context.Background(), "code-abc"); err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if ep.method != http.MethodPost {
				t.Errorf("method = %s, want POST", ep.method)
			}
			want := map[string]string{
				"code":          "code-abc",
				"client_id":     "client-123",
				"client_secret": "secret-456",
				"redirect_uri":  "https://libread.example/auth/callback",
				"grant_type":    "authorization_code",
			}
			for k, v := range want {
				if ep.form.Get(k) != v {
					t.Errorf("form %s = %q, want %q", k, ep.form.Get(k), v)
				}
			}
		})
	}
}

func TestExchangeRejects(t *testing.T) {
	tests := map[string]func(map[string]any){
		"iss が違う":            func(c map[string]any) { c["iss"] = "https://evil.example" },
		"aud が違う":            func(c map[string]any) { c["aud"] = "other-client" },
		"期限切れ":               func(c map[string]any) { c["exp"] = oidcNow.Unix() },
		"exp がない":            func(c map[string]any) { delete(c, "exp") },
		"email_verified が偽":  func(c map[string]any) { c["email_verified"] = false },
		"email_verified がない": func(c map[string]any) { delete(c, "email_verified") },
		"本人以外":               func(c map[string]any) { c["email"] = "someone@example.com" },
		"email がない":          func(c map[string]any) { delete(c, "email") },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			claims := ownerClaims()
			mutate(claims)
			c := newOIDC(t, &fakeTokenEndpoint{idToken: fakeIDToken(t, claims)})
			if err := c.Exchange(context.Background(), "code-abc"); err == nil {
				t.Error("want error")
			}
		})
	}
}

func TestExchangeRejectsTokenEndpointError(t *testing.T) {
	c := newOIDC(t, &fakeTokenEndpoint{status: http.StatusBadRequest, idToken: fakeIDToken(t, ownerClaims())})
	err := c.Exchange(context.Background(), "code-abc")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "secret-456") {
		t.Errorf("エラーにクライアントシークレットが含まれている: %v", err)
	}
}

func TestExchangeRejectsMalformedIDToken(t *testing.T) {
	for _, tok := range []string{"", "only-one-part", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".c"} {
		c := newOIDC(t, &fakeTokenEndpoint{idToken: tok})
		if err := c.Exchange(context.Background(), "code-abc"); err == nil {
			t.Errorf("id_token %q: want error", tok)
		}
	}
}

func TestParseReturnPath(t *testing.T) {
	tests := map[string]string{
		"/":                    "/",
		"/books":               "/books",
		"/libraries":           "/libraries",
		"":                     "/",
		"/login":               "/",
		"https://evil.example": "/",
		"//evil.example":       "/",
		"/books/../x":          "/",
		"books":                "/",
	}
	for in, want := range tests {
		if got := ParseReturnPath(in).String(); got != want {
			t.Errorf("ParseReturnPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewState(t *testing.T) {
	a, err := NewState()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewState()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("NewState が同じ値を返した")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil || len(raw) != 32 {
		t.Errorf("NewState = %q: 32バイトの base64url でない (%d, %v)", a, len(raw), err)
	}
}
