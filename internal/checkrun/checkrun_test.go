package checkrun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"libread/internal/calil"
	"libread/internal/domain"
	"libread/internal/holiday"
	"libread/internal/store"
)

// now は 2026-10-21（水）14:05。
var now = time.Date(2026, 10, 21, 14, 5, 0, 0, domain.Tokyo())

const dataJSON = `{
  "version": 1,
  "books": [{"isbn": "9784003101018", "title": "本のタイトルP", "addedOn": "2026-10-01"}],
  "libraries": [
    {"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館",
     "schedule": {"weekly": {"wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-01"},
     "closures": []}
  ]
}`

const calilFinal = `{"session": "S-1", "continue": 0, "books": {
  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可"}}}}}`

// fakeServices はカーリルと holidays-jp の代わりに応答する。
type fakeServices struct {
	mu          sync.Mutex
	calilStatus int // 0 なら 200
	calil       int // カーリルの所蔵チェックへの要求の回数
}

func (f *fakeServices) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/check":
		f.calil++
		if f.calilStatus != 0 {
			w.WriteHeader(f.calilStatus)
			return
		}
		w.Write([]byte(calilFinal))
	case strings.HasSuffix(r.URL.Path, "/date.json"):
		w.Write([]byte(`{"2026-11-03": "文化の日"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeServices) calilRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calil
}

type fixture struct {
	deps     Deps
	svc      *fakeServices
	cacheDir string
	warnings []string
	data     store.Data
}

func newFixture(t *testing.T, svc *fakeServices) *fixture {
	t.Helper()
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	data, err := store.ParseData([]byte(dataJSON))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{svc: svc, cacheDir: t.TempDir(), data: data}
	cache := store.NewCache(f.cacheDir)
	clock := func() time.Time { return now }
	f.deps = Deps{
		Calil: &calil.Client{
			BaseURL:   srv.URL,
			AppKey:    "test-appkey",
			HTTP:      srv.Client(),
			Now:       clock,
			Sleep:     func(ctx context.Context, d time.Duration) error { return ctx.Err() },
			UserAgent: "libread-test",
		},
		Holidays: &holiday.Client{BaseURL: srv.URL, HTTP: srv.Client(), Cache: cache, Now: clock, UserAgent: "libread-test"},
		Cache:    cache,
		Warn:     func(msg string) { f.warnings = append(f.warnings, msg) },
	}
	return f
}

func (f *fixture) writeHoldingsCache(t *testing.T, fetchedAt string) {
	t.Helper()
	raw := `{"storedAt": "2026-10-21T13:00:00+09:00", "value": {
	  "9784003101018|Tokyo_Setagaya": {"isbn": "9784003101018", "systemId": "Tokyo_Setagaya", "systemStatus": "OK", "byLibKey": {"中央図書館": "館内のみ"}, "fetchedAt": "` + fetchedAt + `"}}}`
	if err := os.WriteFile(filepath.Join(f.cacheDir, "holdings.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) run(t *testing.T) (domain.Result, error) {
	t.Helper()
	return Run(context.Background(), f.deps, f.data, f.data.Books, now)
}

func onlyHolding(t *testing.T, r domain.Result) domain.BookHolding {
	t.Helper()
	libs := append(append([]domain.LibraryResult(nil), r.Readable...), r.NotReadable...)
	if len(libs) != 1 || len(libs[0].Books) != 1 {
		t.Fatalf("result = %+v, want 1 library with 1 book", r)
	}
	return libs[0].Books[0]
}

func TestRunUsesFreshCacheWithoutCalil(t *testing.T) {
	f := newFixture(t, &fakeServices{})
	f.writeHoldingsCache(t, "2026-10-21T13:50:00+09:00")

	r, err := f.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := f.svc.calilRequests(); n != 0 {
		t.Errorf("30分以内のキャッシュがあるのにカーリルへ %d 回問い合わせた", n)
	}
	if h := onlyHolding(t, r); h.Availability != domain.InLibraryOnly || h.Stale {
		t.Errorf("holding = %+v, want キャッシュの館内のみ", h)
	}
	if len(r.Readable) != 1 {
		t.Errorf("readable = %d 館, want 1", len(r.Readable))
	}
	if !r.CheckedAt.Equal(now) {
		t.Errorf("CheckedAt = %v, want %v", r.CheckedAt, now)
	}
}

func TestRunRefetchesStaleOrMissingCacheAndSavesIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cache string // 空ならキャッシュなし
	}{
		{"古いキャッシュ", "2026-10-21T13:00:00+09:00"},
		{"キャッシュなし", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, &fakeServices{})
			if tc.cache != "" {
				f.writeHoldingsCache(t, tc.cache)
			}
			r, err := f.run(t)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if n := f.svc.calilRequests(); n != 1 {
				t.Errorf("カーリルへの問い合わせ = %d 回, want 1（1セッション）", n)
			}
			if h := onlyHolding(t, r); h.Availability != domain.AvailableToLend || h.Stale {
				t.Errorf("holding = %+v, want 取得し直した貸出可", h)
			}
			// 取得した結果はキャッシュに保存し、次は問い合わせない。
			if _, err := f.run(t); err != nil {
				t.Fatalf("2回目の Run: %v", err)
			}
			if n := f.svc.calilRequests(); n != 1 {
				t.Errorf("2回目にもカーリルへ問い合わせた（合計 %d 回）", n)
			}
		})
	}
}

func TestRunFallsBackToOldCacheOnCalilFailure(t *testing.T) {
	f := newFixture(t, &fakeServices{calilStatus: http.StatusServiceUnavailable})
	f.writeHoldingsCache(t, "2026-10-21T13:00:00+09:00")

	r, err := f.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	h := onlyHolding(t, r)
	if !h.Stale {
		t.Errorf("holding = %+v, want 古い情報（Stale）", h)
	}
	if len(r.Readable) != 0 {
		t.Errorf("古い情報の館が読める館に入っている: %+v", r.Readable)
	}
	if len(f.warnings) == 0 || !strings.Contains(strings.Join(f.warnings, "\n"), "カーリル") {
		t.Errorf("warnings = %q, want カーリルの取得失敗の警告", f.warnings)
	}
	if strings.Contains(strings.Join(f.warnings, "\n"), "test-appkey") {
		t.Errorf("警告にアプリケーションキーが含まれている: %q", f.warnings)
	}
}

func TestRunFailsWhenCalilFailsWithoutCache(t *testing.T) {
	f := newFixture(t, &fakeServices{calilStatus: http.StatusServiceUnavailable})

	if _, err := f.run(t); err == nil {
		t.Fatal("want error")
	}
}
