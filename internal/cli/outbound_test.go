package cli

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// outboundRequest は外部へ送った要求1件。
type outboundRequest struct {
	host   string
	url    *url.URL
	header http.Header
	at     time.Time // 注入した時計での送信時刻
}

// outboundRecorder は送ったすべての要求を記録し、サービス（ホスト）ごとの同時要求数の最大値を数える。
// 要求は応答の本文を閉じるまで送信中とみなす。
type outboundRecorder struct {
	next http.RoundTripper
	now  func() time.Time

	mu          sync.Mutex
	requests    []outboundRequest
	inFlight    map[string]int
	maxInFlight map[string]int
}

func (r *outboundRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	r.mu.Lock()
	r.requests = append(r.requests, outboundRequest{host: host, url: req.URL, header: req.Header.Clone(), at: r.now()})
	r.inFlight[host]++
	r.maxInFlight[host] = max(r.maxInFlight[host], r.inFlight[host])
	r.mu.Unlock()

	resp, err := r.next.RoundTrip(req)
	if err != nil {
		r.done(host)
		return nil, err
	}
	resp.Body = &doneBody{ReadCloser: resp.Body, done: func() { r.done(host) }}
	return resp, nil
}

func (r *outboundRecorder) done(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight[host]--
}

// take は記録した要求を返し、記録を空にする。
func (r *outboundRecorder) take() []outboundRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.requests
	r.requests = nil
	return out
}

type doneBody struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (b *doneBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.done)
	return err
}

// outboundData は、メモ付きの本2冊と、祝日を休館とする館を含む2システムの利用図書館。
const outboundData = `{"version": 1,
  "books": [
    {"isbn": "9784003101100", "title": "本のタイトルQ", "note": "秘密のメモQ p.12", "addedOn": "2026-10-01"},
    {"isbn": "9784003101025", "title": "本のタイトルR", "note": "秘密のメモR p.34", "addedOn": "2026-10-02"}
  ],
  "libraries": [
    {"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館",
     "schedule": {"weekly": {"wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "closed"}, "updatedOn": "2026-10-01"},
     "closures": []},
    {"systemId": "Tokyo_Meguro", "libKey": "八雲中央図書館", "name": "目黒区立八雲中央図書館",
     "schedule": {"weekly": {"wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-01"},
     "closures": []}
  ]}`

const outboundRunning = `{"session": "S-1", "continue": 1, "books": {
  "9784003101100": {"Tokyo_Setagaya": {"status": "Running", "libkey": {}}, "Tokyo_Meguro": {"status": "Running", "libkey": {}}},
  "9784003101025": {"Tokyo_Setagaya": {"status": "Running", "libkey": {}}, "Tokyo_Meguro": {"status": "Running", "libkey": {}}}}}`

const outboundFinal = `{"session": "S-1", "continue": 0, "books": {
  "9784003101100": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可"}}, "Tokyo_Meguro": {"status": "OK", "libkey": {}}},
  "9784003101025": {"Tokyo_Setagaya": {"status": "OK", "libkey": {}}, "Tokyo_Meguro": {"status": "OK", "libkey": {"八雲中央図書館": "館内のみ"}}}}}`

// 外部サービスへの配慮（Constitution 技術的制約、research.md R9）を、すべてのコマンドで確かめる。
//   - メモ（読んだ位置）はどの要求にも含まれない。
//   - コマンドごとの要求数が R9 の上限に収まる。
//   - 各サービスへの同時要求は1本まで。
//   - すべての要求に User-Agent: libread/<version> が付く。
func TestOutboundRequests(t *testing.T) {
	holidays, err := os.ReadFile(filepath.Join("..", "holiday", "testdata", "2026.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &fakeServices{
		calil:              []string{outboundRunning, outboundRunning, outboundFinal},
		calilLibrary:       calilFixture(t, "library_setagaya.json"),
		calilSystemLibrary: calilFixture(t, "library_system_setagaya.json"),
		holidays:           string(holidays),
		ndl:                ndlFixture(t, "opensearch_isbn.xml"),
	}
	env, stdout, stderr := newCheckEnv(t, "", svc)
	if err := os.WriteFile(filepath.Join(env.DataDir, "data.json"), []byte(outboundData), 0o600); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: checkStart}
	env.Now, env.Sleep = clock.Now, clock.Sleep
	rec := &outboundRecorder{next: env.HTTPClient.Transport, now: clock.Now, inFlight: map[string]int{}, maxInFlight: map[string]int{}}
	env.HTTPClient = &http.Client{Transport: rec}

	notes := []string{"秘密のメモQ", "秘密のメモR", "秘密のメモS", "秘密のメモT"}

	// want は1回のコマンドで各サービスへ送る要求の数。
	type want struct {
		calilSessions int // カーリル 所蔵チェックのセッション（初回 + ポーリング）
		calilLibrary  int // カーリル 図書館検索
		ndl           int
	}
	steps := []struct {
		name    string
		advance time.Duration // 実行前に時計を進める
		args    []string
		want    want
	}{
		{name: "check（キャッシュなし）", args: []string{"check"}, want: want{calilSessions: 1}},
		{name: "check（30分以内に再実行）", advance: 10 * time.Minute, args: []string{"check"}},
		{name: "check <本>（30分以内）", advance: 19 * time.Minute, args: []string{"check", "本のタイトルQ"}},
		{name: "check（キャッシュが30分を過ぎた）", advance: 2 * time.Minute, args: []string{"check"}, want: want{calilSessions: 1}},
		{name: "book find", args: []string{"book", "find", "吾輩は猫である"}, want: want{ndl: 1}},
		{name: "book add", args: []string{"book", "add", "9784003101018", "--note", "秘密のメモS p.56"}, want: want{ndl: 1}},
		{name: "book note", args: []string{"book", "note", "本のタイトルQ", "秘密のメモT p.78"}},
		{name: "book list", args: []string{"book", "list"}},
		{name: "book rm", args: []string{"book", "rm", "本のタイトルR"}},
		{name: "lib find", args: []string{"lib", "find", "--pref", "東京都", "--city", "世田谷区"}, want: want{calilLibrary: 1}},
		{name: "lib add", args: []string{"lib", "add", "Tokyo_Setagaya:経堂図書館"}, want: want{calilLibrary: 1}},
		{name: "lib hours", args: []string{"lib", "hours", "経堂", "--wed", "09:00-19:00"}},
		{name: "lib closure add", args: []string{"lib", "closure", "add", "経堂", "2026-11-01", "2026-11-03"}},
		{name: "lib closure list", args: []string{"lib", "closure", "list", "経堂"}},
		{name: "lib closure rm", args: []string{"lib", "closure", "rm", "経堂", "1"}},
		{name: "lib list", args: []string{"lib", "list"}},
		{name: "lib rm", args: []string{"lib", "rm", "経堂"}},
		{name: "help", args: []string{"help"}},
	}
	for _, step := range steps {
		clock.Sleep(t.Context(), step.advance)
		if code := run(env, stdout, stderr, step.args...); code != ExitOK {
			t.Fatalf("%s: exit code = %d, stderr = %s", step.name, code, stderr)
		}
		reqs := rec.take()

		var got want
		var checks []outboundRequest
		holidayYears := map[string]int{}
		for _, r := range reqs {
			if ua := r.header.Get("User-Agent"); ua != "libread/"+version {
				t.Errorf("%s: %s の User-Agent = %q, want %q", step.name, r.url, ua, "libread/"+version)
			}
			sent, err := url.QueryUnescape(r.url.String())
			if err != nil {
				t.Fatal(err)
			}
			for _, vs := range r.header {
				sent += "\n" + strings.Join(vs, "\n")
			}
			for _, note := range notes {
				if strings.Contains(sent, note) {
					t.Errorf("%s: メモが外部に送られた: %s", step.name, sent)
				}
			}

			switch {
			case r.host == "api.calil.jp" && r.url.Path == "/check":
				checks = append(checks, r)
			case r.host == "api.calil.jp" && r.url.Path == "/library":
				got.calilLibrary++
			case r.host == "holidays-jp.github.io":
				holidayYears[r.url.Path]++
			case r.host == "ndlsearch.ndl.go.jp":
				got.ndl++
			default:
				t.Errorf("%s: 想定していない要求: %s", step.name, r.url)
			}
		}

		// 所蔵チェックは1セッション: 初回の要求のあとはセッションIDでのポーリングだけで、間隔は2秒以上。
		for i, r := range checks {
			q := r.url.Query()
			if i == 0 {
				got.calilSessions++
				if !q.Has("isbn") || !q.Has("systemid") || q.Has("session") {
					t.Errorf("%s: 初回の要求 = %s", step.name, r.url)
				}
				continue
			}
			if q.Get("session") != "S-1" || q.Has("isbn") || q.Has("systemid") {
				t.Errorf("%s: %d 回目の要求が同じセッションのポーリングでない: %s", step.name, i+1, r.url)
			}
			if d := r.at.Sub(checks[i-1].at); d < 2*time.Second {
				t.Errorf("%s: ポーリングの間隔 = %v, want 2秒以上", step.name, d)
			}
		}
		if len(checks) > 0 && checks[len(checks)-1].at.Sub(checks[0].at) > 8*time.Second {
			t.Errorf("%s: 所蔵チェックのセッションが8秒を超えた", step.name)
		}
		for year, n := range holidayYears {
			if n > 1 {
				t.Errorf("%s: holidays-jp の %s に %d 回要求した, want 年ごとに1回まで", step.name, year, n)
			}
		}
		if got != step.want {
			t.Errorf("%s: 要求数 = %+v, want %+v", step.name, got, step.want)
		}
	}

	for host, n := range rec.maxInFlight {
		if n > 1 {
			t.Errorf("%s への同時要求 = %d, want 1本まで", host, n)
		}
	}
	// 祝日を休館とする館があるので、最初の check で一度だけ祝日を取得し、以降はキャッシュを使う。
	if n := countHost(svc.recorded(), "holidays-jp.github.io"); n != 1 {
		t.Errorf("holidays-jp への要求 = %d, want 1（30日以内はキャッシュを使う）", n)
	}
}

func countHost(reqs []recordedRequest, host string) int {
	n := 0
	for _, r := range reqs {
		if r.host == host {
			n++
		}
	}
	return n
}
