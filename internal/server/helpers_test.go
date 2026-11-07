package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"libread/internal/auth"
	"libread/internal/domain"
	"libread/internal/store"
)

const (
	testOrigin = "https://libread.example"
	testOwner  = "owner@example.com"
)

var (
	testSessionKey = []byte("0123456789abcdef0123456789abcdef")
	// testNow は 2026-11-05（木）17:00。
	testNow = time.Date(2026, 11, 5, 17, 0, 0, 0, domain.Tokyo())
)

// syncBuffer はログの書き込み先。ハンドラーは並行に動くことがあるので排他する。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testServer struct {
	handler http.Handler
	cfg     Config
	log     *syncBuffer
}

func newTestServer(t *testing.T, mutate ...func(*Config)) *testServer {
	t.Helper()
	log := &syncBuffer{}
	cfg := Config{
		DataDir:  t.TempDir(),
		CacheDir: t.TempDir(),
		Static: fstest.MapFS{
			"index.html":  {Data: []byte("<!doctype html><title>libread</title>")},
			"assets/x.js": {Data: []byte("console.log(1)")},
		},
		Auth: auth.Config{
			ClientID:     "client-123",
			ClientSecret: "secret-456",
			PublicOrigin: testOrigin,
			OwnerEmail:   testOwner,
			AuthURL:      "https://accounts.example/o/oauth2/v2/auth",
			TokenURL:     "http://127.0.0.1:1/token", // 使うテストで差し替える
			HTTP:         http.DefaultClient,
			Now:          func() time.Time { return testNow },
		},
		SessionKey:  testSessionKey,
		CalilAppKey: "test-appkey",
		Now:         func() time.Time { return testNow },
		HTTP:        http.DefaultClient,
		Log:         log,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	return &testServer{handler: New(cfg), cfg: cfg, log: log}
}

// writeData は利用者データ（data.json）を置く。
func (s *testServer) writeData(t *testing.T, doc string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.cfg.DataDir, "data.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sessionCookie は今の世代で発行した有効なセッション Cookie を返す。
func (s *testServer) sessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	return s.sessionCookieAt(t, testNow)
}

func (s *testServer) sessionCookieAt(t *testing.T, issuedAt time.Time) *http.Cookie {
	t.Helper()
	st, err := store.LoadAuthState(s.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := auth.NewSessionGeneration(st.Generation)
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := auth.Issue(testSessionKey, gen, issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: value}
}

type reqOpt func(*http.Request)

func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }

func withOrigin(origin string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Origin", origin) }
}

func (s *testServer) do(t *testing.T, method, target, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// cookieNamed は応答で設定された name の Cookie を返す。
func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
