package holiday

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"libread/internal/domain"
	"libread/internal/store"
)

// holidays2026 は holidays-jp の 2026/date.json の実際の応答（testdata/2026.json に記録）。
func holidays2026(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "2026.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const testUserAgent = "libread/test"

type fakeServer struct {
	*httptest.Server
	mu     sync.Mutex
	paths  []string
	agents []string
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

func newFakeServer(t *testing.T, status int, body string) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.agents = append(f.agents, r.Header.Get("User-Agent"))
		f.mu.Unlock()
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func newClient(t *testing.T, baseURL string, cache store.Cache, now time.Time) *Client {
	t.Helper()
	return &Client{
		BaseURL:   baseURL,
		HTTP:      http.DefaultClient,
		Cache:     cache,
		Now:       func() time.Time { return now },
		UserAgent: testUserAgent,
	}
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

var now = time.Date(2026, 10, 21, 14, 5, 0, 0, domain.Tokyo())

func TestLookup(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, holidays2026(t))
	c := newClient(t, srv.URL, store.NewCache(t.TempDir()), now)
	isHoliday := c.Lookup(context.Background())

	tests := []struct {
		date        string
		wantHoliday bool
	}{
		{date: "2026-10-12", wantHoliday: true},
		{date: "2026-11-03", wantHoliday: true},
		{date: "2026-05-06", wantHoliday: true}, // 振替休日
		{date: "2026-09-22", wantHoliday: true}, // 国民の休日
		{date: "2026-12-23", wantHoliday: false},
		{date: "2026-10-21", wantHoliday: false},
	}
	for _, tt := range tests {
		got, known := isHoliday(mustDate(t, tt.date))
		if !known {
			t.Errorf("%s: known = false", tt.date)
		}
		if got != tt.wantHoliday {
			t.Errorf("%s: isHoliday = %v, want %v", tt.date, got, tt.wantHoliday)
		}
	}
	if n := srv.count(); n != 1 {
		t.Errorf("同じ年の問い合わせは1回まで: %d 回", n)
	}
	if len(srv.paths) == 0 || srv.paths[0] != "/2026/date.json" {
		t.Errorf("path = %v, want /2026/date.json", srv.paths)
	}
	for _, ua := range srv.agents {
		if ua != testUserAgent {
			t.Errorf("User-Agent = %q, want %q", ua, testUserAgent)
		}
	}
}

func TestLookupUsesCacheWithin30Days(t *testing.T) {
	cache := store.NewCache(t.TempDir())
	if err := store.Put(cache, "holidays-2026.json", map[string]string{"2026-10-21": "テスト用の祝日"}, now.Add(-29*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, http.StatusOK, holidays2026(t))
	isHoliday := newClient(t, srv.URL, cache, now).Lookup(context.Background())

	got, known := isHoliday(mustDate(t, "2026-10-21"))
	if !known || !got {
		t.Errorf("キャッシュの内容が使われていない: (%v, %v)", got, known)
	}
	if n := srv.count(); n != 0 {
		t.Errorf("30日以内のキャッシュがあるのに %d 回問い合わせた", n)
	}
}

func TestLookupRefreshesAfter30Days(t *testing.T) {
	cache := store.NewCache(t.TempDir())
	if err := store.Put(cache, "holidays-2026.json", map[string]string{"2026-10-21": "古い祝日"}, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, http.StatusOK, holidays2026(t))
	isHoliday := newClient(t, srv.URL, cache, now).Lookup(context.Background())

	if got, known := isHoliday(mustDate(t, "2026-10-21")); !known || got {
		t.Errorf("再取得した内容が使われていない: (%v, %v)", got, known)
	}
	if n := srv.count(); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
	stored, storedAt, ok, err := store.Get[map[string]string](cache, "holidays-2026.json")
	if err != nil || !ok {
		t.Fatalf("キャッシュが保存されていない: ok=%v err=%v", ok, err)
	}
	if !storedAt.Equal(now) || stored["2026-10-12"] != "スポーツの日" {
		t.Errorf("キャッシュが更新されていない: storedAt=%v value=%v", storedAt, stored)
	}
}

func TestLookupFailureWithoutCacheIsUnknown(t *testing.T) {
	srv := newFakeServer(t, http.StatusInternalServerError, "")
	isHoliday := newClient(t, srv.URL, store.NewCache(t.TempDir()), now).Lookup(context.Background())

	if _, known := isHoliday(mustDate(t, "2026-10-21")); known {
		t.Error("取得に失敗しキャッシュもないのに known = true")
	}
}

func TestLookupFailureFallsBackToOldCache(t *testing.T) {
	cache := store.NewCache(t.TempDir())
	if err := store.Put(cache, "holidays-2026.json", map[string]string{"2026-10-21": "古い祝日"}, now.Add(-60*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, http.StatusInternalServerError, "")
	isHoliday := newClient(t, srv.URL, cache, now).Lookup(context.Background())

	if got, known := isHoliday(mustDate(t, "2026-10-21")); !known || !got {
		t.Errorf("取得に失敗したときは古いキャッシュを使う: (%v, %v)", got, known)
	}
}

func TestLookupOutOfRangeYearIsUnknown(t *testing.T) {
	// holidays-jp は前年・今年・翌年しか提供しない（research.md R4）。
	srv := newFakeServer(t, http.StatusNotFound, "")
	isHoliday := newClient(t, srv.URL, store.NewCache(t.TempDir()), now).Lookup(context.Background())

	if _, known := isHoliday(mustDate(t, "2030-01-01")); known {
		t.Error("取得できない年なのに known = true")
	}
}

func TestLookupMalformedResponseIsUnknown(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, `["not", "an", "object"]`)
	isHoliday := newClient(t, srv.URL, store.NewCache(t.TempDir()), now).Lookup(context.Background())

	if _, known := isHoliday(mustDate(t, "2026-10-21")); known {
		t.Error("形式の違う応答なのに known = true")
	}
}
