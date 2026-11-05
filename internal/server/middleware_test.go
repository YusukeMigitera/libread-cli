package server

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"libread/internal/auth"
	"libread/internal/store"
)

const unauthenticatedBody = `{"error":{"code":"unauthenticated","message":"ログインしてください"}}`

const sampleData = `{"version": 1,
  "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "note": "ひみつのメモ", "addedOn": "2026-10-21"}],
  "libraries": [{"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館", "closures": []}]}`

var apiPaths = []string{
	"/api/session",
	"/api/session/logout",
	"/api/session/revoke-all",
	"/api/check",
	"/api/books",
	"/api/books/9784003101018",
	"/api/books/9784003101019",
	"/api/libraries",
	"/api/libraries/Tokyo_Setagaya%3A%E4%B8%AD%E5%A4%AE%E5%9B%B3%E6%9B%B8%E9%A4%A8",
	"/api/import",
	"/api/nonexistent",
	"/api/",
}

var methods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

// FR-110・SC-104: 本人確認ができない要求には、どの URL・どのメソッドでも、データの有無によらず同じ 401 を返す。
func TestUnauthenticatedAPIIsUniform(t *testing.T) {
	for _, withData := range []bool{false, true} {
		s := newTestServer(t)
		if withData {
			s.writeData(t, sampleData)
		}
		for _, path := range apiPaths {
			for _, m := range methods {
				rec := s.do(t, m, path, `{}`, withOrigin(testOrigin))
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("data=%v %s %s: status = %d, want 401", withData, m, path, rec.Code)
				}
				if got := strings.TrimSpace(rec.Body.String()); got != unauthenticatedBody {
					t.Errorf("data=%v %s %s: body = %s", withData, m, path, got)
				}
				if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
					t.Errorf("%s %s: Content-Type = %q", m, path, ct)
				}
			}
		}
	}
}

func TestInvalidSessionsGet401(t *testing.T) {
	s := newTestServer(t)
	s.writeData(t, sampleData)

	expired := s.sessionCookieAt(t, testNow.Add(-31*24*time.Hour))
	old := s.sessionCookie(t)
	if err := store.SaveAuthState(s.cfg.DataDir, store.AuthState{Generation: 2}); err != nil {
		t.Fatal(err)
	}
	gen2, _ := auth.NewSessionGeneration(2)
	otherKey, _, _ := auth.Issue([]byte("another-key-another-key-another-k"), gen2, testNow)

	for name, c := range map[string]string{
		"期限切れ":  expired.Value,
		"世代が古い": old.Value,
		"別の鍵":   otherKey,
		"壊れた値":  "garbage",
	} {
		rec := s.do(t, http.MethodGet, "/api/books", "", withCookie(&http.Cookie{Name: sessionCookieName, Value: c}))
		if rec.Code != http.StatusUnauthorized || strings.TrimSpace(rec.Body.String()) != unauthenticatedBody {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestWritesRequireMatchingOrigin(t *testing.T) {
	s := newTestServer(t)
	c := s.sessionCookie(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for name, opts := range map[string][]reqOpt{
			"Origin なし":   {withCookie(c)},
			"別の Origin":   {withCookie(c), withOrigin("https://evil.example")},
			"ポートが違う":      {withCookie(c), withOrigin(testOrigin + ":8443")},
			"null Origin": {withCookie(c), withOrigin("null")},
		} {
			rec := s.do(t, m, "/api/session/logout", `{}`, opts...)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d, want 403", m, name, rec.Code)
				continue
			}
			if !strings.Contains(rec.Body.String(), `"code":"forbidden"`) {
				t.Errorf("%s %s: body = %s", m, name, rec.Body)
			}
		}
	}
	// GET は Origin がなくてよい。
	if rec := s.do(t, http.MethodGet, "/api/session", "", withCookie(c)); rec.Code != http.StatusOK {
		t.Errorf("GET /api/session: status = %d, want 200", rec.Code)
	}
	// 一致すれば通る。
	if rec := s.do(t, http.MethodPost, "/api/session/logout", "", withCookie(c), withOrigin(testOrigin)); rec.Code != http.StatusNoContent {
		t.Errorf("POST /api/session/logout: status = %d, want 204", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestServer(t)
	c := s.sessionCookie(t)
	for _, tc := range []struct {
		path    string
		opts    []reqOpt
		noStore bool
	}{
		{"/", nil, false},
		{"/assets/x.js", nil, false},
		{"/api/books", nil, true},
		{"/api/session", []reqOpt{withCookie(c)}, true},
		{"/api/nonexistent", []reqOpt{withCookie(c)}, true},
		{"/auth/login?return=/books", nil, true},
		{"/auth/callback?error=access_denied", nil, true},
	} {
		rec := s.do(t, http.MethodGet, tc.path, "", tc.opts...)
		if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
			t.Errorf("%s: Strict-Transport-Security = %q", tc.path, got)
		}
		if got := rec.Header().Get("Cache-Control"); tc.noStore && got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", tc.path, got)
		}
	}
}

var logLine = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d) (GET|POST|PUT|PATCH|DELETE) (\S+) (\d{3}) \d+(\.\d+)?(ns|µs|ms|s)$`)

func TestRequestLog(t *testing.T) {
	s := newTestServer(t)
	s.writeData(t, sampleData)
	c := s.sessionCookie(t)

	s.do(t, http.MethodGet, "/api/book-search?q=ひみつの書名&author=夏目", "", withCookie(c))
	s.do(t, http.MethodPatch, "/api/books/9784003101018", `{"note": "ひみつのメモ"}`, withCookie(c), withOrigin(testOrigin))
	s.do(t, http.MethodGet, "/api/books?x=1", "")
	s.do(t, http.MethodGet, "/auth/callback?code=secret-code&state=secret-state", "")

	log := s.log.String()
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) != 4 {
		t.Fatalf("ログの行数 = %d, want 4:\n%s", len(lines), log)
	}
	wantPaths := []string{"/api/book-search", "/api/books/9784003101018", "/api/books", "/auth/callback"}
	for i, l := range lines {
		m := logLine.FindStringSubmatch(l)
		if m == nil {
			t.Errorf("ログの形式が違う: %q", l)
			continue
		}
		if m[4] != wantPaths[i] {
			t.Errorf("パス = %q, want %q", m[4], wantPaths[i])
		}
	}
	if !strings.Contains(lines[2], " 401 ") {
		t.Errorf("状態コードがない: %q", lines[2])
	}
	for _, secret := range []string{"ひみつ", "夏目", "?", "secret-code", "secret-state", c.Value, testOwner, "test-appkey"} {
		if strings.Contains(log, secret) {
			t.Errorf("ログに %q が含まれている:\n%s", secret, log)
		}
	}
	if bytes.Contains([]byte(log), []byte("Cookie")) {
		t.Errorf("ログに Cookie が含まれている:\n%s", log)
	}
}
