package cli

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"libread/internal/domain"
)

var update = flag.Bool("update", false, "ゴールデンファイルを実際の出力で書き換える")

const testAppKey = "test-appkey"

// checkStart は check のテストの現在時刻（2026-10-21 水曜 14:05）。
var checkStart = time.Date(2026, 10, 21, 14, 5, 0, 0, domain.Tokyo())

// fakeClock は Sleep で時刻を進める。実際には待たない。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
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
	c.now = c.now.Add(d)
	return nil
}

// fakeServices はカーリル・holidays-jp・NDL サーチの代わりに応答する。Host ヘッダでサービスを見分ける。
type fakeServices struct {
	mu                 sync.Mutex
	calil              []string // i 番目の要求に返す応答。尽きたら最後の応答を返し続ける
	calilStatus        int
	calilLibrary       string // 図書館検索（/library）の応答
	calilSystemLibrary string // systemid を指定した図書館検索の応答
	holidays           string
	holidaysStatus     int
	ndl                string
	ndlStatus          int
	requests           []recordedRequest
	calilRequests      int
}

type recordedRequest struct {
	host  string
	url   *url.URL
	agent string
}

func (f *fakeServices) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, recordedRequest{host: r.Host, url: r.URL, agent: r.Header.Get("User-Agent")})
	switch r.Host {
	case "api.calil.jp":
		if r.URL.Path == "/library" && r.URL.Query().Has("systemid") {
			w.Write([]byte(f.calilSystemLibrary))
			return
		}
		if r.URL.Path == "/library" {
			w.Write([]byte(f.calilLibrary))
			return
		}
		f.calilRequests++
		if f.calilStatus != 0 && f.calilStatus != http.StatusOK {
			w.WriteHeader(f.calilStatus)
			return
		}
		if len(f.calil) == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(f.calil[min(f.calilRequests, len(f.calil))-1]))
	case "holidays-jp.github.io":
		if f.holidaysStatus != 0 && f.holidaysStatus != http.StatusOK {
			w.WriteHeader(f.holidaysStatus)
			return
		}
		w.Write([]byte(f.holidays))
	case "ndlsearch.ndl.go.jp":
		if f.ndlStatus != 0 && f.ndlStatus != http.StatusOK {
			w.WriteHeader(f.ndlStatus)
			return
		}
		w.Write([]byte(f.ndl))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeServices) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

// routeTransport はすべての要求を target へ送る。元のホスト名は Host ヘッダに残す。
type routeTransport struct {
	target *url.URL
}

func (t routeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.Host = r.URL.Host
	out.URL.Scheme = t.target.Scheme
	out.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

// newCheckEnv は dataFile（testdata 内）を data.json として置き、svc に接続する Env を返す。
func newCheckEnv(t *testing.T, dataFile string, svc *fakeServices) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: checkStart}
	var stdout, stderr bytes.Buffer
	env := Env{
		Stdout:      &stdout,
		Stderr:      &stderr,
		Now:         clock.Now,
		Sleep:       clock.Sleep,
		HTTPClient:  &http.Client{Transport: routeTransport{target: target}},
		DataDir:     t.TempDir(),
		CacheDir:    t.TempDir(),
		CalilAppKey: testAppKey,
	}
	if dataFile != "" {
		raw, err := os.ReadFile(filepath.Join("testdata", dataFile))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(env.DataDir, "data.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return env, &stdout, &stderr
}

// writeHoldingsCache は所蔵キャッシュ（holdings.json）を書く。キーは ISBN|SystemID。
func writeHoldingsCache(t *testing.T, env Env, entries string) {
	t.Helper()
	raw := `{"storedAt": "2026-10-21T13:00:00+09:00", "value": {` + entries + `}}`
	if err := os.WriteFile(filepath.Join(env.CacheDir, "holdings.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("出力が %s と一致しない\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// 以下のカーリルの応答は check API（format=json&callback=no）と同じ形式。

const exampleRunning = `{"session": "S-1", "continue": 1, "books": {
  "9784003101018": {"Tokyo_Setagaya": {"status": "Running", "libkey": {}}},
  "9784003101100": {"Tokyo_Setagaya": {"status": "Running", "libkey": {}}}}}`

const exampleFinal = `{"session": "S-1", "continue": 0, "books": {
  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可", "経堂図書館": "貸出可", "奥沢図書館": "館内のみ", "梅丘図書館": "貸出可", "烏山図書館": "蔵書あり"}}},
  "9784003101100": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "館内のみ", "経堂図書館": "貸出中", "奥沢図書館": "予約中"}}}}}`

// TestCheckContractExample は contracts/cli.md の出力例を再現する
// （US1 受け入れシナリオ 2〜4、開館前、閉館まで30分未満、臨時休館、開館情報未入力、予約中、所蔵なし）。
func TestCheckContractExample(t *testing.T) {
	svc := &fakeServices{calil: []string{exampleRunning, exampleFinal}}
	env, stdout, stderr := newCheckEnv(t, "check_example.json", svc)

	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "check_example.golden", stdout.String())

	reqs := svc.recorded()
	if svc.calilRequests != 2 {
		t.Errorf("カーリルへの要求 = %d, want 2（初回 + ポーリング1回）", svc.calilRequests)
	}
	for _, r := range reqs {
		if r.host == "holidays-jp.github.io" {
			t.Error("祝日の扱いがすべて「曜日どおり」なのに holidays-jp に問い合わせた")
		}
		if !strings.HasPrefix(r.agent, "libread/") {
			t.Errorf("User-Agent = %q", r.agent)
		}
		if strings.Contains(r.url.String(), "p.120") {
			t.Errorf("メモが外部に送られた: %s", r.url)
		}
	}
	first := reqs[0].url.Query()
	if first.Get("isbn") != "9784003101018,9784003101100" || first.Get("systemid") != "Tokyo_Setagaya" || first.Get("appkey") != testAppKey {
		t.Errorf("初回の要求 = %s", reqs[0].url)
	}
}

// US1 受け入れシナリオ 1: 本を指定して検索し、読める館・休館・貸出中を区別する。
func TestCheckScenario1SingleBook(t *testing.T) {
	final := `{"session": "S-1", "continue": 0, "books": {
	  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可", "梅丘図書館": "貸出可", "経堂図書館": "貸出中"}}}}}`
	svc := &fakeServices{calil: []string{final}}
	env, stdout, stderr := newCheckEnv(t, "check_scenario1.json", svc)

	if code := Run(context.Background(), []string{"check", "タイトルP"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "check_scenario1.golden", stdout.String())
	if q := svc.recorded()[0].url.Query(); q.Get("isbn") != "9784003101018" {
		t.Errorf("指定した本だけを問い合わせていない: isbn = %q", q.Get("isbn"))
	}
}

// US1 受け入れシナリオ 5: どの館でも読めない。
func TestCheckScenario5NoneReadable(t *testing.T) {
	final := `{"session": "S-1", "continue": 0, "books": {
	  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出中", "梅丘図書館": "貸出可", "経堂図書館": "予約中"}}},
	  "9784003101100": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"経堂図書館": "貸出中"}}}}}`
	svc := &fakeServices{calil: []string{final}}
	env, stdout, stderr := newCheckEnv(t, "check_scenario1.json", svc)

	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "check_none.golden", stdout.String())
}

// 本日終了、祝日判定不可、取得タイムアウト、取得失敗。
func TestCheckEdgeCases(t *testing.T) {
	running := `{"session": "S-1", "continue": 1, "books": {
	  "9784003101018": {
	    "Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可", "経堂図書館": "貸出可"}},
	    "Tokyo_Meguro": {"status": "Running", "libkey": {}},
	    "Kanagawa_Kawasaki": {"status": "Error", "libkey": {}}}}}`
	svc := &fakeServices{calil: []string{running}, holidaysStatus: http.StatusInternalServerError}
	env, stdout, stderr := newCheckEnv(t, "check_edge.json", svc)

	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "check_edge.golden", stdout.String())
	if got := env.Now().Sub(checkStart); got > 8*time.Second {
		t.Errorf("カーリルへの問い合わせに %v かかった（上限 8 秒）", got)
	}
}

func TestCheckReusesFreshCache(t *testing.T) {
	svc := &fakeServices{}
	env, stdout, stderr := newCheckEnv(t, "check_single.json", svc)
	writeHoldingsCache(t, env, `
	  "9784003101018|Tokyo_Setagaya": {"isbn": "9784003101018", "systemId": "Tokyo_Setagaya", "systemStatus": "OK", "byLibKey": {"中央図書館": "貸出可"}, "fetchedAt": "2026-10-21T13:50:00+09:00"},
	  "9784003101018|Tokyo_Meguro": {"isbn": "9784003101018", "systemId": "Tokyo_Meguro", "systemStatus": "Cache", "byLibKey": {}, "fetchedAt": "2026-10-21T13:50:00+09:00"}`)

	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if n := len(svc.recorded()); n != 0 {
		t.Errorf("30分以内のキャッシュがあるのに %d 回問い合わせた", n)
	}
	for _, want := range []string{"所蔵情報: カーリル 13:50 取得（30分以内の取得結果を使用）", "1. 世田谷区立中央図書館  開館中 〜19:00", "『本のタイトルP』 所蔵なし"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("出力に %q がない:\n%s", want, stdout)
		}
	}
}

func TestCheckStaleCacheAndFailedRefetch(t *testing.T) {
	svc := &fakeServices{calilStatus: http.StatusServiceUnavailable}
	env, stdout, stderr := newCheckEnv(t, "check_single.json", svc)
	writeHoldingsCache(t, env, `
	  "9784003101018|Tokyo_Setagaya": {"isbn": "9784003101018", "systemId": "Tokyo_Setagaya", "systemStatus": "OK", "byLibKey": {"中央図書館": "貸出可"}, "fetchedAt": "2026-10-21T13:00:00+09:00"}`)

	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "check_stale.golden", stdout.String())
	if !strings.Contains(stderr.String(), "カーリル") {
		t.Errorf("取得に失敗したことが標準エラー出力に書かれていない: %q", stderr)
	}
}

func TestCheckFallsBackToCachePerSystem(t *testing.T) {
	// 中央図書館（世田谷）はキャッシュがないので再取得する。八雲中央図書館（目黒）は再取得に失敗したので、30分以内のキャッシュを使う。
	final := `{"session": "S-1", "continue": 0, "books": {
	  "9784003101018": {
	    "Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可"}},
	    "Tokyo_Meguro": {"status": "Error", "libkey": {}}}}}`
	svc := &fakeServices{calil: []string{final}}
	env, stdout, stderr := newCheckEnv(t, "check_single.json", svc)
	writeHoldingsCache(t, env, `
	  "9784003101018|Tokyo_Meguro": {"isbn": "9784003101018", "systemId": "Tokyo_Meguro", "systemStatus": "OK", "byLibKey": {"八雲中央図書館": "館内のみ"}, "fetchedAt": "2026-10-21T13:50:00+09:00"}`)

	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "check_mixed_times.golden", stdout.String())

	// 再取得できた結果はキャッシュに保存し、次回は問い合わせない。
	stdout.Reset()
	before := svc.calilRequests
	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("2回目の exit code = %d, stderr = %s", code, stderr)
	}
	if svc.calilRequests != before {
		t.Errorf("2回目に %d 回問い合わせた", svc.calilRequests-before)
	}
}

func TestCheckFetchFailureWithoutCache(t *testing.T) {
	svc := &fakeServices{calilStatus: http.StatusServiceUnavailable}
	env, _, stderr := newCheckEnv(t, "check_single.json", svc)

	if code := Run(context.Background(), []string{"check"}, env); code != ExitError {
		t.Errorf("exit code = %d, want %d（外部サービスに全く接続できない）", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "カーリル") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCheckRequiresAppKey(t *testing.T) {
	svc := &fakeServices{}
	env, _, stderr := newCheckEnv(t, "check_single.json", svc)
	env.CalilAppKey = ""

	if code := Run(context.Background(), []string{"check"}, env); code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "LIBREAD_CALIL_APPKEY") {
		t.Errorf("キーの設定方法が案内されていない: %q", stderr)
	}
	if n := len(svc.recorded()); n != 0 {
		t.Errorf("キーがないのに %d 回問い合わせた", n)
	}
}

func TestCheckWithoutLibraries(t *testing.T) {
	env, _, stderr := newCheckEnv(t, "", &fakeServices{})
	if err := os.WriteFile(filepath.Join(env.DataDir, "data.json"), []byte(`{"version": 1, "books": [{"isbn": "9784003101018", "title": "P", "addedOn": "2026-10-01"}], "libraries": []}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := Run(context.Background(), []string{"check"}, env); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "libread lib find") {
		t.Errorf("図書館の登録が案内されていない: %q", stderr)
	}
}

func TestCheckWithoutBooks(t *testing.T) {
	env, _, stderr := newCheckEnv(t, "", &fakeServices{})
	if err := os.WriteFile(filepath.Join(env.DataDir, "data.json"), []byte(`{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "N", "closures": []}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := Run(context.Background(), []string{"check"}, env); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "libread book find") {
		t.Errorf("本の登録が案内されていない: %q", stderr)
	}
}

func TestCheckUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "未登録の本", args: []string{"check", "三四郎"}},
		{name: "引数が多すぎる", args: []string{"check", "本のタイトルP", "余分"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _, _ := newCheckEnv(t, "check_single.json", &fakeServices{})
			if code := Run(context.Background(), tt.args, env); code != ExitUsage {
				t.Errorf("exit code = %d, want %d", code, ExitUsage)
			}
		})
	}
}

// 引数なしの libread は check と同じ（contracts/cli.md）。
func TestRunWithoutArgsRunsCheck(t *testing.T) {
	svc := &fakeServices{calil: []string{exampleRunning, exampleFinal}}
	env, stdout, stderr := newCheckEnv(t, "check_example.json", svc)

	if code := Run(context.Background(), nil, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "check_example.golden", stdout.String())
}
