package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 外部サービスへの配慮（Constitution 技術的制約、001 research R9、research W9）を、すべての API で確かめる。
// 001 の internal/cli/outbound_test.go を API に移したもの。

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

func newOutboundRecorder(next http.RoundTripper, now func() time.Time) *outboundRecorder {
	return &outboundRecorder{next: next, now: now, inFlight: map[string]int{}, maxInFlight: map[string]int{}}
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

func (r *outboundRecorder) maxInFlightByHost() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]int{}
	for h, n := range r.maxInFlight {
		out[h] = n
	}
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

// outboundServices は、カーリル（所蔵・図書館）・holidays-jp・NDL サーチをまとめて受ける。
// 所蔵と祝日は fakeServices、図書館は fakeCalilLibrary、書誌は fakeNDL に任せる。
type outboundServices struct {
	check   *fakeServices
	library *fakeCalilLibrary
	ndl     *fakeNDL
}

func (o *outboundServices) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Host == "api.calil.jp" && r.URL.Path == "/library":
		o.library.ServeHTTP(w, r)
	case r.Host == "ndlsearch.ndl.go.jp":
		o.ndl.ServeHTTP(w, r)
	default:
		o.check.ServeHTTP(w, r)
	}
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

// outboundServer は outboundServices に接続し、送った要求を記録するサーバー。
type outboundServer struct {
	*checkServer
	svc *outboundServices
	rec *outboundRecorder
}

func newOutboundServer(t *testing.T, calilResponses ...string) *outboundServer {
	t.Helper()
	holidays, err := os.ReadFile(filepath.Join("..", "holiday", "testdata", "2026.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &outboundServices{
		check:   &fakeServices{calil: calilResponses, holidays: string(holidays)},
		library: &fakeCalilLibrary{},
		ndl:     &fakeNDL{},
	}
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: checkStart}
	rec := newOutboundRecorder(routeTransport{target: target}, clock.Now)
	ts := newTestServer(t, func(c *Config) {
		c.Now = clock.Now
		c.Sleep = clock.Sleep
		c.HTTP = &http.Client{Transport: rec}
	})
	s := &outboundServer{
		checkServer: &checkServer{testServer: ts, clock: clock, cookie: ts.sessionCookieAt(t, checkStart)},
		svc:         svc,
		rec:         rec,
	}
	s.writeData(t, outboundData)
	return s
}

func (s *outboundServer) send(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	opts := []reqOpt{withCookie(s.cookie)}
	if isWrite(method) {
		opts = append(opts, withOrigin(testOrigin))
	}
	return s.do(t, method, target, body, opts...)
}

var outboundNotes = []string{"秘密のメモQ", "秘密のメモR", "秘密のメモS", "秘密のメモT"}

// outboundCounts は1回の API で各サービスへ送る要求の数。
type outboundCounts struct {
	calilSessions int // カーリル 所蔵チェックのセッション（初回 + ポーリング）
	calilLibrary  int // カーリル 図書館 API
	ndl           int
}

// checkOutbound は reqs が配慮を守っていることを確かめ、サービスごとの要求数を返す。
func checkOutbound(t *testing.T, step string, reqs []outboundRequest) outboundCounts {
	t.Helper()
	var got outboundCounts
	var checks []outboundRequest
	holidayYears := map[string]int{}
	for _, r := range reqs {
		if ua := r.header.Get("User-Agent"); ua != userAgent {
			t.Errorf("%s: %s の User-Agent = %q, want %q", step, r.url, ua, userAgent)
		}
		sent, err := url.QueryUnescape(r.url.String())
		if err != nil {
			t.Fatal(err)
		}
		for _, vs := range r.header {
			sent += "\n" + strings.Join(vs, "\n")
		}
		for _, note := range outboundNotes {
			if strings.Contains(sent, note) {
				t.Errorf("%s: メモが外部に送られた: %s", step, sent)
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
			t.Errorf("%s: 想定していない要求: %s", step, r.url)
		}
	}

	// 所蔵チェックのセッションは、初回の要求（isbn・systemid を指定）のあとセッションIDでのポーリングだけが続き、
	// ポーリングの間隔は2秒以上、1セッションは8秒以内。セッションは重ならない（同時要求は1本まで）。
	var sessionStart outboundRequest
	for i, r := range checks {
		q := r.url.Query()
		if q.Has("isbn") {
			got.calilSessions++
			sessionStart = r
			if !q.Has("systemid") || q.Has("session") {
				t.Errorf("%s: 初回の要求 = %s", step, r.url)
			}
			continue
		}
		if i == 0 || q.Get("session") != "S-1" || q.Has("systemid") {
			t.Errorf("%s: %d 回目の要求が同じセッションのポーリングでない: %s", step, i+1, r.url)
			continue
		}
		if d := r.at.Sub(checks[i-1].at); d < 2*time.Second {
			t.Errorf("%s: ポーリングの間隔 = %v, want 2秒以上", step, d)
		}
		if r.at.Sub(sessionStart.at) > 8*time.Second {
			t.Errorf("%s: 所蔵チェックのセッションが8秒を超えた", step)
		}
	}
	for year, n := range holidayYears {
		if n > 1 {
			t.Errorf("%s: holidays-jp の %s に %d 回要求した, want 年ごとに1回まで", step, year, n)
		}
	}
	return got
}

// すべての API を順に呼び、次を確かめる。
//   - メモ（読んだ位置）はどの要求にも含まれない。
//   - API ごとの要求数が 001 research R9 の上限に収まる（/api/check は1セッションで、30分以内の再検索ではカーリルに問い合わせない。
//     図書館の検索・登録はカーリルへ1回、本の検索・登録は NDL サーチへ1回）。
//   - 各サービスへの同時要求は1本まで。
//   - すべての要求に User-Agent: libread/<version> が付く。
func TestOutboundRequests(t *testing.T) {
	if !strings.HasPrefix(userAgent, "libread/") {
		t.Fatalf("User-Agent %q が libread/<version> の形でない", userAgent)
	}
	s := newOutboundServer(t, outboundRunning, outboundRunning, outboundFinal)

	steps := []struct {
		name    string
		advance time.Duration // 実行前に時計を進める
		method  string
		target  string
		body    string
		want    outboundCounts
	}{
		{name: "check（キャッシュなし）", method: http.MethodGet, target: "/api/check", want: outboundCounts{calilSessions: 1}},
		{name: "check（30分以内に再検索）", advance: 10 * time.Minute, method: http.MethodGet, target: "/api/check"},
		{name: "check?isbn（30分以内）", advance: 19 * time.Minute, method: http.MethodGet, target: "/api/check?isbn=9784003101100"},
		{name: "check（キャッシュが30分を過ぎた）", advance: 2 * time.Minute, method: http.MethodGet, target: "/api/check", want: outboundCounts{calilSessions: 1}},
		{name: "book-search", method: http.MethodGet, target: "/api/book-search?q=" + url.QueryEscape("吾輩は猫である"), want: outboundCounts{ndl: 1}},
		{name: "books POST", method: http.MethodPost, target: "/api/books", body: `{"isbn": "9784003101018", "note": "秘密のメモS p.56"}`, want: outboundCounts{ndl: 1}},
		{name: "books PATCH", method: http.MethodPatch, target: "/api/books/9784003101100", body: `{"note": "秘密のメモT p.78"}`},
		{name: "books GET", method: http.MethodGet, target: "/api/books"},
		{name: "books DELETE", method: http.MethodDelete, target: "/api/books/9784003101025"},
		{name: "library-search", method: http.MethodGet, target: "/api/library-search?pref=" + url.QueryEscape("東京都") + "&city=" + url.QueryEscape("世田谷区"), want: outboundCounts{calilLibrary: 1}},
		{name: "libraries POST", method: http.MethodPost, target: "/api/libraries", body: `{"id": "Tokyo_Setagaya:経堂図書館"}`, want: outboundCounts{calilLibrary: 1}},
		{name: "schedule PUT", method: http.MethodPut, target: libraryPath(kyodoID, "/schedule"), body: `{"weekly": ` + fullWeek + `, "holiday": {"mode": "same"}}`},
		{name: "closures POST", method: http.MethodPost, target: libraryPath(kyodoID, "/closures"), body: `{"start": "2026-11-01", "end": "2026-11-03"}`},
		{name: "closures DELETE", method: http.MethodDelete, target: libraryPath(kyodoID, "/closures/0")},
		{name: "libraries GET", method: http.MethodGet, target: "/api/libraries"},
		{name: "libraries DELETE", method: http.MethodDelete, target: libraryPath(kyodoID)},
		{name: "session GET", method: http.MethodGet, target: "/api/session"},
		{name: "画面", method: http.MethodGet, target: "/"},
	}
	for _, step := range steps {
		s.clock.Sleep(t.Context(), step.advance)
		if rec := s.send(t, step.method, step.target, step.body); rec.Code >= 300 {
			t.Fatalf("%s: status = %d, body = %s", step.name, rec.Code, rec.Body)
		}
		if got := checkOutbound(t, step.name, s.rec.take()); got != step.want {
			t.Errorf("%s: 要求数 = %+v, want %+v", step.name, got, step.want)
		}
	}

	for host, n := range s.rec.maxInFlightByHost() {
		if n > 1 {
			t.Errorf("%s への同時要求 = %d, want 1本まで", host, n)
		}
	}
	// 祝日を休館とする館があるので、最初の check で一度だけ祝日を取得し、以降はキャッシュを使う。
	if n := countHost(s.svc.check.recorded(), "holidays-jp.github.io"); n != 1 {
		t.Errorf("holidays-jp への要求 = %d, want 1（30日以内はキャッシュを使う）", n)
	}
}

// 画面からは複数の API が同時に呼ばれる。そのときも、各サービスへの同時要求は1本までで、メモは送られない。
func TestOutboundRequestsWhenAPIsOverlap(t *testing.T) {
	s := newOutboundServer(t, outboundRunning, outboundFinal)
	s.svc.check.delay = 5 * time.Millisecond

	calls := []struct{ method, target, body string }{
		{http.MethodGet, "/api/check", ""},
		{http.MethodGet, "/api/check?isbn=9784003101025", ""},
		{http.MethodGet, "/api/library-search?pref=" + url.QueryEscape("東京都") + "&city=" + url.QueryEscape("世田谷区"), ""},
		{http.MethodPost, "/api/libraries", `{"id": "Tokyo_Setagaya:経堂図書館"}`},
		{http.MethodGet, "/api/book-search?q=" + url.QueryEscape("吾輩は猫である"), ""},
		{http.MethodPost, "/api/books", `{"isbn": "9784003101018", "note": "秘密のメモS p.56"}`},
		{http.MethodPatch, "/api/books/9784003101100", `{"note": "秘密のメモT p.78"}`},
	}
	var wg sync.WaitGroup
	codes := make([]int, len(calls))
	for i, c := range calls {
		wg.Go(func() { codes[i] = s.send(t, c.method, c.target, c.body).Code })
	}
	wg.Wait()
	for i, code := range codes {
		if code >= 300 {
			t.Errorf("%s %s: status = %d", calls[i].method, calls[i].target, code)
		}
	}

	got := checkOutbound(t, "同時の呼び出し", s.rec.take())
	if got.ndl != 2 || got.calilLibrary != 2 {
		t.Errorf("要求数 = %+v, want NDL サーチ2回・カーリルの図書館 API 2回", got)
	}
	for host, n := range s.rec.maxInFlightByHost() {
		if n > 1 {
			t.Errorf("%s への同時要求 = %d, want 1本まで", host, n)
		}
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
