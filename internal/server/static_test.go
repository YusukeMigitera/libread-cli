package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestStaticIndexWithoutSession(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/", "/index.html"} {
		rec := s.do(t, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<title>libread</title>") {
			t.Errorf("%s: body = %q", path, rec.Body)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", path, got)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Errorf("%s: Content-Type = %q", path, got)
		}
	}
}

func TestStaticAssetsAreImmutable(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/assets/x.js", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestStaticMissingIs404(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/assets/missing.js", "/missing.txt"} {
		rec := s.do(t, http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); strings.Contains(got, "immutable") {
			t.Errorf("%s: 404 に Cache-Control = %q が付いている", path, got)
		}
	}
}
