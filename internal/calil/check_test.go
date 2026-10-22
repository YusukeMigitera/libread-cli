package calil

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"libread/internal/domain"
)

// 以下の応答は、カーリル 図書館API の check（format=json&callback=no）と同じ形式。
// TODO(T022): 実レスポンスを testdata/check_*.json に記録し、それを使うように置き換える。
const (
	checkInitial = `{
  "session": "S-0001",
  "continue": 1,
  "books": {
    "9784003101018": {
      "Tokyo_Setagaya": {"status": "Running", "reserveurl": "", "libkey": {}},
      "Tokyo_Meguro": {"status": "Cache", "reserveurl": "https://example.jp/meguro", "libkey": {"八雲中央": "貸出中"}},
      "Kanagawa_Kawasaki": {"status": "Running", "reserveurl": "", "libkey": {}}
    },
    "9784003101100": {
      "Tokyo_Setagaya": {"status": "Running", "reserveurl": "", "libkey": {}},
      "Tokyo_Meguro": {"status": "Running", "reserveurl": "", "libkey": {}},
      "Kanagawa_Kawasaki": {"status": "Running", "reserveurl": "", "libkey": {}}
    }
  }
}`
	checkPoll = `{
  "session": "S-0001",
  "continue": 1,
  "books": {
    "9784003101018": {
      "Tokyo_Setagaya": {"status": "OK", "reserveurl": "https://example.jp/setagaya", "libkey": {"中央": "貸出可", "経堂": "館内のみ"}},
      "Tokyo_Meguro": {"status": "Cache", "reserveurl": "https://example.jp/meguro", "libkey": {"八雲中央": "貸出中"}},
      "Kanagawa_Kawasaki": {"status": "Running", "reserveurl": "", "libkey": {}}
    },
    "9784003101100": {
      "Tokyo_Setagaya": {"status": "Running", "reserveurl": "", "libkey": {}},
      "Tokyo_Meguro": {"status": "Running", "reserveurl": "", "libkey": {}},
      "Kanagawa_Kawasaki": {"status": "Running", "reserveurl": "", "libkey": {}}
    }
  }
}`
	checkFinal = `{
  "session": "S-0001",
  "continue": 0,
  "books": {
    "9784003101018": {
      "Tokyo_Setagaya": {"status": "OK", "reserveurl": "https://example.jp/setagaya", "libkey": {"中央": "貸出可", "経堂": "館内のみ"}},
      "Tokyo_Meguro": {"status": "Cache", "reserveurl": "https://example.jp/meguro", "libkey": {"八雲中央": "貸出中"}},
      "Kanagawa_Kawasaki": {"status": "Error", "reserveurl": "", "libkey": {}}
    },
    "9784003101100": {
      "Tokyo_Setagaya": {"status": "OK", "reserveurl": "", "libkey": {}},
      "Tokyo_Meguro": {"status": "OK", "reserveurl": "", "libkey": {"八雲中央": "予約中"}},
      "Kanagawa_Kawasaki": {"status": "Error", "reserveurl": "", "libkey": {}}
    }
  }
}`
	checkAlwaysRunning = `{
  "session": "S-0002",
  "continue": 1,
  "books": {
    "9784003101018": {
      "Tokyo_Setagaya": {"status": "OK", "reserveurl": "", "libkey": {"中央": "貸出可"}},
      "Tokyo_Meguro": {"status": "Running", "reserveurl": "", "libkey": {}}
    }
  }
}`
)

const (
	testAppKey    = "test-appkey"
	testUserAgent = "libread/test"
)

var (
	isbnA   = domain.ISBN("9784003101018")
	isbnB   = domain.ISBN("9784003101100")
	start   = time.Date(2026, 10, 21, 14, 5, 0, 0, domain.Tokyo())
	systems = []string{"Tokyo_Setagaya", "Tokyo_Meguro", "Kanagawa_Kawasaki"}
)

// fakeClock は Sleep で時刻を進める。実際には待たない。
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

type request struct {
	at    time.Time
	path  string
	query url.Values
	agent string
}

type fakeCalil struct {
	*httptest.Server
	clock     *fakeClock
	mu        sync.Mutex
	requests  []request
	responses []string // i 番目の要求に返す応答。尽きたら最後の応答を返し続ける
	status    int
}

func newFakeCalil(t *testing.T, clock *fakeClock, responses ...string) *fakeCalil {
	t.Helper()
	f := &fakeCalil{clock: clock, responses: responses, status: http.StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, request{at: clock.Now(), path: r.URL.Path, query: r.URL.Query(), agent: r.Header.Get("User-Agent")})
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			return
		}
		i := min(len(f.requests), len(f.responses)) - 1
		w.Write([]byte(f.responses[i]))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeCalil) recorded() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.requests...)
}

func newClient(baseURL string, clock *fakeClock) *Client {
	return &Client{
		BaseURL:   baseURL,
		AppKey:    testAppKey,
		HTTP:      http.DefaultClient,
		Now:       clock.Now,
		Sleep:     clock.Sleep,
		UserAgent: testUserAgent,
	}
}

func findSnapshot(t *testing.T, snaps []domain.HoldingSnapshot, isbn domain.ISBN, system string) domain.HoldingSnapshot {
	t.Helper()
	for _, s := range snaps {
		if s.ISBN == isbn && s.SystemID == system {
			return s
		}
	}
	t.Fatalf("スナップショット %s|%s がない", isbn, system)
	return domain.HoldingSnapshot{}
}

func TestCheckSendsOneSessionAndPolls(t *testing.T) {
	clock := &fakeClock{now: start}
	srv := newFakeCalil(t, clock, checkInitial, checkPoll, checkFinal)

	snaps, err := newClient(srv.URL, clock).Check(context.Background(), []domain.ISBN{isbnA, isbnB}, systems)
	if err != nil {
		t.Fatal(err)
	}

	reqs := srv.recorded()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3（初回 + ポーリング2回）", len(reqs))
	}
	first := reqs[0]
	if first.path != "/check" {
		t.Errorf("path = %q, want /check", first.path)
	}
	wantFirst := map[string]string{
		"appkey":   testAppKey,
		"isbn":     "9784003101018,9784003101100",
		"systemid": "Tokyo_Setagaya,Tokyo_Meguro,Kanagawa_Kawasaki",
		"format":   "json",
		"callback": "no",
	}
	for k, v := range wantFirst {
		if got := first.query.Get(k); got != v {
			t.Errorf("初回の %s = %q, want %q", k, got, v)
		}
	}
	if first.query.Has("session") {
		t.Error("初回の要求に session が付いている")
	}
	for i, r := range reqs[1:] {
		if got := r.query.Get("session"); got != "S-0001" {
			t.Errorf("ポーリング %d の session = %q, want S-0001", i+1, got)
		}
		for k, v := range map[string]string{"appkey": testAppKey, "format": "json", "callback": "no"} {
			if got := r.query.Get(k); got != v {
				t.Errorf("ポーリング %d の %s = %q, want %q", i+1, k, got, v)
			}
		}
		if r.query.Has("isbn") || r.query.Has("systemid") {
			t.Errorf("ポーリング %d に isbn・systemid が付いている（新しいセッションになる）", i+1)
		}
	}
	for i := 1; i < len(reqs); i++ {
		if gap := reqs[i].at.Sub(reqs[i-1].at); gap < 2*time.Second {
			t.Errorf("要求 %d と %d の間隔 = %v, want ≥ 2s", i-1, i, gap)
		}
	}
	for i, r := range reqs {
		if r.agent != testUserAgent {
			t.Errorf("要求 %d の User-Agent = %q, want %q", i, r.agent, testUserAgent)
		}
	}

	if len(snaps) != 6 {
		t.Fatalf("snapshots = %d, want 6（2冊 × 3システム）", len(snaps))
	}
	fetchedAt := clock.Now()
	tests := []struct {
		isbn     domain.ISBN
		system   string
		status   domain.SystemStatus
		byLibKey map[string]string
	}{
		{isbnA, "Tokyo_Setagaya", domain.SystemOK, map[string]string{"中央": "貸出可", "経堂": "館内のみ"}},
		{isbnA, "Tokyo_Meguro", domain.SystemCache, map[string]string{"八雲中央": "貸出中"}},
		{isbnA, "Kanagawa_Kawasaki", domain.SystemError, map[string]string{}},
		{isbnB, "Tokyo_Setagaya", domain.SystemOK, map[string]string{}},
		{isbnB, "Tokyo_Meguro", domain.SystemOK, map[string]string{"八雲中央": "予約中"}},
	}
	for _, tt := range tests {
		s := findSnapshot(t, snaps, tt.isbn, tt.system)
		if s.SystemStatus != tt.status {
			t.Errorf("%s|%s status = %q, want %q", tt.isbn, tt.system, s.SystemStatus, tt.status)
		}
		if len(s.ByLibKey) != len(tt.byLibKey) {
			t.Errorf("%s|%s ByLibKey = %v, want %v", tt.isbn, tt.system, s.ByLibKey, tt.byLibKey)
		}
		for k, v := range tt.byLibKey {
			if s.ByLibKey[k] != v {
				t.Errorf("%s|%s ByLibKey[%s] = %q, want %q", tt.isbn, tt.system, k, s.ByLibKey[k], v)
			}
		}
		if !s.FetchedAt.Equal(fetchedAt) {
			t.Errorf("%s|%s FetchedAt = %v, want %v", tt.isbn, tt.system, s.FetchedAt, fetchedAt)
		}
	}
}

func TestCheckStopsAtDeadline(t *testing.T) {
	clock := &fakeClock{now: start}
	srv := newFakeCalil(t, clock, checkAlwaysRunning)

	snaps, err := newClient(srv.URL, clock).Check(context.Background(), []domain.ISBN{isbnA}, []string{"Tokyo_Setagaya", "Tokyo_Meguro"})
	if err != nil {
		t.Fatal(err)
	}

	reqs := srv.recorded()
	deadline := start.Add(8 * time.Second)
	for i, r := range reqs {
		if !r.at.Before(deadline) {
			t.Errorf("要求 %d が8秒の上限を過ぎてから送られた: %v", i, r.at.Sub(start))
		}
	}
	if len(reqs) < 2 {
		t.Errorf("requests = %d, 上限までポーリングしていない", len(reqs))
	}
	if elapsed := clock.Now().Sub(start); elapsed > 8*time.Second {
		t.Errorf("経過時間 = %v, want ≤ 8s", elapsed)
	}
	for _, d := range clock.sleeps {
		if d < 2*time.Second {
			t.Errorf("ポーリング間隔 %v < 2s", d)
		}
	}
	if s := findSnapshot(t, snaps, isbnA, "Tokyo_Meguro"); s.SystemStatus != domain.SystemRunning {
		t.Errorf("上限で打ち切ったシステムの status = %q, want Running（取得タイムアウト）", s.SystemStatus)
	}
	if s := findSnapshot(t, snaps, isbnA, "Tokyo_Setagaya"); s.SystemStatus != domain.SystemOK || s.ByLibKey["中央"] != "貸出可" {
		t.Errorf("完了済みのシステムの結果が失われた: %+v", s)
	}
}

func TestCheckHonoursContextCancel(t *testing.T) {
	clock := &fakeClock{now: start}
	srv := newFakeCalil(t, clock, checkAlwaysRunning)
	ctx, cancel := context.WithCancel(context.Background())
	c := newClient(srv.URL, clock)
	c.Sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return clock.Sleep(ctx, d)
	}

	_, err := c.Check(ctx, []domain.ISBN{isbnA}, []string{"Tokyo_Setagaya"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestCheckHTTPError(t *testing.T) {
	clock := &fakeClock{now: start}
	srv := newFakeCalil(t, clock, checkFinal)
	srv.mu.Lock()
	srv.status = http.StatusInternalServerError
	srv.mu.Unlock()

	if _, err := newClient(srv.URL, clock).Check(context.Background(), []domain.ISBN{isbnA}, systems); err == nil {
		t.Error("HTTP 500 なのにエラーにならない")
	}
}

func TestCheckMalformedJSON(t *testing.T) {
	clock := &fakeClock{now: start}
	srv := newFakeCalil(t, clock, `{"session": "S", "continue": "yes"}`)

	if _, err := newClient(srv.URL, clock).Check(context.Background(), []domain.ISBN{isbnA}, systems); err == nil {
		t.Error("形式の違う JSON なのにエラーにならない")
	}
}

func TestCheckMissingEntryIsError(t *testing.T) {
	// 応答に問い合わせた本・システムがなければ、取得できなかったものとして扱う。
	clock := &fakeClock{now: start}
	srv := newFakeCalil(t, clock, `{"session": "S", "continue": 0, "books": {"9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {}}}}}`)

	snaps, err := newClient(srv.URL, clock).Check(context.Background(), []domain.ISBN{isbnA, isbnB}, []string{"Tokyo_Setagaya", "Tokyo_Meguro"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 4 {
		t.Fatalf("snapshots = %d, want 4", len(snaps))
	}
	if s := findSnapshot(t, snaps, isbnA, "Tokyo_Meguro"); s.SystemStatus != domain.SystemError {
		t.Errorf("応答にないシステムの status = %q, want Error", s.SystemStatus)
	}
	if s := findSnapshot(t, snaps, isbnB, "Tokyo_Setagaya"); s.SystemStatus != domain.SystemError {
		t.Errorf("応答にない本の status = %q, want Error", s.SystemStatus)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestCheckErrorDoesNotLeakAppKey(t *testing.T) {
	clock := &fakeClock{now: start}
	c := newClient("https://api.calil.jp", clock)
	c.HTTP = &http.Client{Transport: failingTransport{}}

	_, err := c.Check(context.Background(), []domain.ISBN{isbnA}, systems)
	if err == nil {
		t.Fatal("接続できないのにエラーにならない")
	}
	if strings.Contains(err.Error(), testAppKey) {
		t.Errorf("エラーにアプリケーションキーが含まれる: %v", err)
	}
}
