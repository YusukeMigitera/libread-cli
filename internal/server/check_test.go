package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"libread/internal/calil"
)

// checkStart は検索のテストの現在時刻（2026-10-21 水曜 14:05）。001 の CLI のテストと同じ。
var checkStart = time.Date(2026, 10, 21, 14, 5, 0, 0, time.FixedZone("JST", 9*60*60))

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
	mu             sync.Mutex
	calil          []string // i 番目の所蔵の要求に返す応答。尽きたら最後の応答を返し続ける
	calilStatus    int
	calilLibrary   string // 図書館検索（/library）の応答
	holidays       string
	holidaysStatus int
	requests       []recordedRequest
	calilRequests  int // 所蔵の問い合わせ（/check）の数
	calilSessions  int // そのうち、新しいセッションを始めた要求（isbn を指定した要求）の数

	// delay は、カーリルへの要求に応答するまでの実時間。同時の要求を重ねるテストで使う。
	delay time.Duration
	// holidaysStarted が nil でなければ、holidays-jp への最初の要求を受けたときに閉じる。
	// そのあと holidaysRelease が閉じられるまで応答しない。
	holidaysStarted, holidaysRelease chan struct{}
	holidaysOnce                     sync.Once
	// calilInFlight・calilMaxInFlight は、同時に処理中のカーリルへの要求の数と、その最大値。
	calilInFlight, calilMaxInFlight atomic.Int32
}

type recordedRequest struct {
	host  string
	url   *url.URL
	agent string
}

func (f *fakeServices) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Host == "api.calil.jp" {
		n := f.calilInFlight.Add(1)
		defer f.calilInFlight.Add(-1)
		for {
			m := f.calilMaxInFlight.Load()
			if n <= m || f.calilMaxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
	}
	if r.Host == "holidays-jp.github.io" && f.holidaysStarted != nil {
		f.holidaysOnce.Do(func() { close(f.holidaysStarted) })
		<-f.holidaysRelease
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, recordedRequest{host: r.Host, url: r.URL, agent: r.Header.Get("User-Agent")})
	switch r.Host {
	case "api.calil.jp":
		if r.URL.Path == "/library" {
			w.Write([]byte(f.calilLibrary))
			return
		}
		f.calilRequests++
		if r.URL.Query().Has("isbn") {
			f.calilSessions++
		}
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
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeServices) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func (f *fakeServices) sessions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calilSessions
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

// checkServer は検索のテスト用のサーバー。外部サービスは svc、時刻は clock。
type checkServer struct {
	*testServer
	clock  *fakeClock
	cookie *http.Cookie
}

// newCheckServer は svc に接続し、時刻を start に固定したサーバーを作る。
func newCheckServer(t *testing.T, svc *fakeServices, start time.Time, mutate ...func(*Config)) *checkServer {
	t.Helper()
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: start}
	all := append([]func(*Config){func(c *Config) {
		c.Now = clock.Now
		c.Sleep = clock.Sleep
		c.HTTP = &http.Client{Transport: routeTransport{target: target}}
	}}, mutate...)
	ts := newTestServer(t, all...)
	return &checkServer{testServer: ts, clock: clock, cookie: ts.sessionCookieAt(t, start)}
}

// writeDataFile は testdata/name を data.json として置く。
func (s *checkServer) writeDataFile(t *testing.T, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	s.writeData(t, string(raw))
}

// writeHoldingsCache は所蔵キャッシュ（holdings.json）を書く。キーは ISBN|SystemID。
func (s *checkServer) writeHoldingsCache(t *testing.T, entries string) {
	t.Helper()
	raw := `{"storedAt": "2026-10-21T13:00:00+09:00", "value": {` + entries + `}}`
	if err := os.WriteFile(filepath.Join(s.cfg.CacheDir, "holdings.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (s *checkServer) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, http.MethodGet, target, "", withCookie(s.cookie))
}

// checkJSON は GET /api/check の応答（200）を JSON として読む。
func (s *checkServer) checkJSON(t *testing.T, target string) map[string]any {
	t.Helper()
	rec := s.get(t, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body = %s", target, rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("応答が JSON でない: %v\n%s", err, rec.Body)
	}
	return out
}

// 契約の例（contracts/http-api.md「検索」）と同じ館・本。中央図書館は水曜 09:00〜19:00、経堂図書館は開館情報未入力。
const contractCheckData = `{"version": 1,
  "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "note": "p.120まで", "addedOn": "2026-10-21"}],
  "libraries": [
    {"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館",
     "schedule": {"weekly": {"wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-21"},
     "closures": []},
    {"systemId": "Tokyo_Setagaya", "libKey": "経堂図書館", "name": "世田谷区立経堂図書館", "closures": []}]}`

const contractCheckFinal = `{"session": "S-1", "continue": 0, "books": {
  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可"}}}}}`

func TestCheckResponseMatchesContract(t *testing.T) {
	svc := &fakeServices{calil: []string{contractCheckFinal}}
	s := newCheckServer(t, svc, checkStart)
	s.writeData(t, contractCheckData)

	rec := s.get(t, "/api/check")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertMatchesExample(t, "check", rec.Body.Bytes())

	var got checkJSONBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CheckedAt != "2026-10-21T14:05:00+09:00" {
		t.Errorf("checkedAt = %q", got.CheckedAt)
	}
	if len(got.Readable) != 1 || len(got.NotReadable) != 1 {
		t.Fatalf("readable = %d 館, notReadable = %d 館, want 1・1", len(got.Readable), len(got.NotReadable))
	}
	central := got.Readable[0]
	if central.Library.ID != "Tokyo_Setagaya:中央図書館" || central.Library.Name != "世田谷区立中央図書館" {
		t.Errorf("library = %+v", central.Library)
	}
	if central.Opening.Status != "open" || central.Opening.Label != "開館中 〜19:00" || central.Opening.Open != "09:00" || central.Opening.Close != "19:00" {
		t.Errorf("opening = %+v", central.Opening)
	}
	book := central.Books[0]
	if book.ISBN != "9784003101018" || book.Title != "吾輩は猫である" || book.Availability != "availableToLend" ||
		book.Label != "在架（貸出可）" || !book.Readable || book.Stale || book.FetchedAt != "2026-10-21T14:05:00+09:00" {
		t.Errorf("book = %+v", book)
	}
	if got.Sources.Holdings.Label != "所蔵情報: カーリル 14:05 取得（30分以内の取得結果を使用）" {
		t.Errorf("holdings.label = %q", got.Sources.Holdings.Label)
	}
	if len(got.Sources.Holdings.Links) != 1 || got.Sources.Holdings.Links[0].URL != "https://calil.jp/book/9784003101018" ||
		got.Sources.Holdings.Links[0].Title != "吾輩は猫である" {
		t.Errorf("holdings.links = %+v", got.Sources.Holdings.Links)
	}
	if got.Sources.OpeningInfo.Label != "開館情報: 利用者入力（最終更新 中央図書館 2026-10-21）" ||
		got.Sources.OpeningInfo.Note != "※ 臨時休館は入力済みの情報のみ反映しています" {
		t.Errorf("openingInfo = %+v", got.Sources.OpeningInfo)
	}
	if got.Sources.Holidays.Label != "祝日: holidays-jp（https://holidays-jp.github.io/）" {
		t.Errorf("holidays = %+v", got.Sources.Holidays)
	}

	// メモは外部に送らない（FR-114）。
	for _, r := range svc.recorded() {
		if strings.Contains(r.url.String(), "p.120") {
			t.Errorf("メモが外部に送られた: %s", r.url)
		}
		if !strings.HasPrefix(r.agent, "libread/") {
			t.Errorf("User-Agent = %q", r.agent)
		}
	}
}

// checkJSONBody は GET /api/check の応答を読むための型（テスト用）。
type checkJSONBody struct {
	CheckedAt   string              `json:"checkedAt"`
	Readable    []checkJSONLibrary  `json:"readable"`
	NotReadable []checkJSONLibrary  `json:"notReadable"`
	Sources     checkJSONSourceBody `json:"sources"`
}

type checkJSONLibrary struct {
	Library struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"library"`
	Opening struct {
		Status string `json:"status"`
		Label  string `json:"label"`
		Open   string `json:"open"`
		Close  string `json:"close"`
	} `json:"opening"`
	Books []struct {
		ISBN         string `json:"isbn"`
		Title        string `json:"title"`
		Availability string `json:"availability"`
		Label        string `json:"label"`
		Readable     bool   `json:"readable"`
		Stale        bool   `json:"stale"`
		FetchedAt    string `json:"fetchedAt"`
	} `json:"books"`
}

type checkJSONSourceBody struct {
	Holdings struct {
		Label string `json:"label"`
		Links []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"links"`
	} `json:"holdings"`
	OpeningInfo struct {
		Label string `json:"label"`
		Note  string `json:"note"`
	} `json:"openingInfo"`
	Holidays struct {
		Label string `json:"label"`
	} `json:"holidays"`
}

// 001 の CLI の契約の例と同じデータ（開館中・開館前・閉館まで30分未満・臨時休館・開館情報未入力）。
const exampleRunning = `{"session": "S-1", "continue": 1, "books": {
  "9784003101018": {"Tokyo_Setagaya": {"status": "Running", "libkey": {}}},
  "9784003101100": {"Tokyo_Setagaya": {"status": "Running", "libkey": {}}}}}`

const exampleFinal = `{"session": "S-1", "continue": 0, "books": {
  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可", "経堂図書館": "貸出可", "奥沢図書館": "館内のみ", "梅丘図書館": "貸出可", "烏山図書館": "蔵書あり"}}},
  "9784003101100": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "館内のみ", "経堂図書館": "貸出中", "奥沢図書館": "予約中"}}}}}`

// opening のキーは状態によって決まる。books は今日行けない館では空。
func TestCheckOpeningFieldsAndBooksByStatus(t *testing.T) {
	svc := &fakeServices{calil: []string{exampleRunning, exampleFinal}}
	s := newCheckServer(t, svc, checkStart)
	s.writeDataFile(t, "check_example.json")

	body := s.checkJSON(t, "/api/check")
	seen := map[string]bool{}
	for _, group := range []string{"readable", "notReadable"} {
		for _, l := range body[group].([]any) {
			lib := l.(map[string]any)
			opening := lib["opening"].(map[string]any)
			status := opening["status"].(string)
			seen[status] = true
			_, hasOpen := opening["open"]
			_, hasClose := opening["close"]
			_, hasClosingSoon := opening["closingSoon"]
			withHours := status == "open" || status == "notYetOpen" || status == "finishedToday"
			if hasOpen != withHours || hasClose != withHours {
				t.Errorf("%s: open・close の有無 = %v・%v, want %v", status, hasOpen, hasClose, withHours)
			}
			if hasClosingSoon != (status == "open") {
				t.Errorf("%s: closingSoon の有無 = %v", status, hasClosingSoon)
			}
			books := lib["books"].([]any)
			if group == "notReadable" && status != "open" && status != "notYetOpen" && len(books) != 0 {
				t.Errorf("今日行けない館（%s）に books がある: %v", status, books)
			}
			if group == "readable" && len(books) == 0 {
				t.Errorf("読める館に books がない: %v", lib)
			}
		}
	}
	for _, want := range []string{"open", "notYetOpen", "closedPeriod", "noSchedule"} {
		if !seen[want] {
			t.Errorf("状態 %s の館がない（テストのデータを確かめる）: %v", want, seen)
		}
	}
	// 閉館まで30分未満の経堂図書館。
	for _, l := range body["readable"].([]any) {
		lib := l.(map[string]any)
		if lib["library"].(map[string]any)["name"] == "世田谷区立経堂図書館" {
			if lib["opening"].(map[string]any)["closingSoon"] != true {
				t.Errorf("経堂図書館の closingSoon = %v, want true", lib["opening"])
			}
		}
	}
}

// 古い情報は読める扱いにしない（Constitution 原則 IV）。取得できなかった本には fetchedAt を付けない。
func TestCheckStaleAndUnfetchedBooks(t *testing.T) {
	svc := &fakeServices{calilStatus: http.StatusServiceUnavailable}
	s := newCheckServer(t, svc, checkStart)
	s.writeDataFile(t, "check_single.json")
	s.writeHoldingsCache(t, `
	  "9784003101018|Tokyo_Setagaya": {"isbn": "9784003101018", "systemId": "Tokyo_Setagaya", "systemStatus": "OK", "byLibKey": {"中央図書館": "貸出可"}, "fetchedAt": "2026-10-21T13:00:00+09:00"}`)

	body := s.checkJSON(t, "/api/check")
	if r := body["readable"].([]any); len(r) != 0 {
		t.Errorf("readable = %v, want 空", r)
	}
	var sawStale, sawUnfetched bool
	for _, l := range body["notReadable"].([]any) {
		for _, b := range l.(map[string]any)["books"].([]any) {
			book := b.(map[string]any)
			_, hasFetchedAt := book["fetchedAt"]
			switch {
			case book["stale"] == true:
				sawStale = true
				if book["readable"] != false {
					t.Errorf("stale なのに readable: %v", book)
				}
				if book["fetchedAt"] != "2026-10-21T13:00:00+09:00" {
					t.Errorf("古い情報の fetchedAt = %v", book["fetchedAt"])
				}
			default:
				sawUnfetched = true
				if hasFetchedAt {
					t.Errorf("取得できなかった本に fetchedAt がある: %v", book)
				}
				if book["availability"] != "unknown" || book["label"] != "不明（取得失敗）" {
					t.Errorf("取得できなかった本 = %v", book)
				}
			}
		}
	}
	if !sawStale || !sawUnfetched {
		t.Errorf("古い情報の本 = %v、取得できなかった本 = %v（どちらもあるはず）", sawStale, sawUnfetched)
	}
}

func TestCheckSetupRequired(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"データなし", "", "check-setup-libraries"},
		{"図書館も本もない", `{"version": 1, "books": [], "libraries": []}`, "check-setup-libraries"},
		{"図書館がない", `{"version": 1, "books": [{"isbn": "9784003101018", "title": "P", "addedOn": "2026-10-01"}], "libraries": []}`, "check-setup-libraries"},
		{"本がない", `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "N", "closures": []}]}`, "check-setup-books"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeServices{}
			s := newCheckServer(t, svc, checkStart)
			if tt.data != "" {
				s.writeData(t, tt.data)
			}
			rec := s.get(t, "/api/check")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			assertMatchesExample(t, tt.want, rec.Body.Bytes())
			want := strings.TrimPrefix(tt.want, "check-setup-")
			var got struct {
				SetupRequired string `json:"setupRequired"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.SetupRequired != want {
				t.Errorf("body = %s, want setupRequired %q", rec.Body, want)
			}
			if n := len(svc.recorded()); n != 0 {
				t.Errorf("登録が足りないのに外部に %d 回問い合わせた", n)
			}
		})
	}
}

func TestCheckSingleBook(t *testing.T) {
	final := `{"session": "S-1", "continue": 0, "books": {
	  "9784003101100": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "館内のみ"}}}}}`
	svc := &fakeServices{calil: []string{final}}
	s := newCheckServer(t, svc, checkStart)
	s.writeDataFile(t, "check_example.json")

	var got checkJSONBody
	raw, _ := json.Marshal(s.checkJSON(t, "/api/check?isbn=9784003101100"))
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, l := range append(got.Readable, got.NotReadable...) {
		for _, b := range l.Books {
			if b.ISBN != "9784003101100" {
				t.Errorf("指定していない本 %s が含まれる", b.ISBN)
			}
		}
	}
	if links := got.Sources.Holdings.Links; len(links) != 1 || links[0].Title != "本のタイトルQ" {
		t.Errorf("links = %+v, want 本のタイトルQ だけ", links)
	}
	if q := svc.recorded()[0].url.Query(); q.Get("isbn") != "9784003101100" {
		t.Errorf("指定した本だけを問い合わせていない: isbn = %q", q.Get("isbn"))
	}
}

func TestCheckErrors(t *testing.T) {
	tests := []struct {
		name   string
		target string
		mutate func(*Config)
		svc    *fakeServices
		status int
		code   string
	}{
		{name: "登録していない本", target: "/api/check?isbn=9784003101032", svc: &fakeServices{}, status: http.StatusNotFound, code: "notFound"},
		{name: "ISBN が不正", target: "/api/check?isbn=9784003101019", svc: &fakeServices{}, status: http.StatusBadRequest, code: "invalid"},
		{name: "ISBN が空", target: "/api/check?isbn=", svc: &fakeServices{}, status: http.StatusBadRequest, code: "invalid"},
		{name: "カーリルのキーがない", target: "/api/check", mutate: func(c *Config) { c.CalilAppKey = "" }, svc: &fakeServices{}, status: http.StatusServiceUnavailable, code: "notConfigured"},
		{name: "カーリルに接続できずキャッシュもない", target: "/api/check", svc: &fakeServices{calilStatus: http.StatusServiceUnavailable}, status: http.StatusBadGateway, code: "upstream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mutate []func(*Config)
			if tt.mutate != nil {
				mutate = append(mutate, tt.mutate)
			}
			s := newCheckServer(t, tt.svc, checkStart, mutate...)
			s.writeDataFile(t, "check_single.json")
			rec := s.get(t, tt.target)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.status, rec.Body)
			}
			var got errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Error.Code != tt.code || got.Error.Message == "" {
				t.Errorf("error = %+v, want code %q", got.Error, tt.code)
			}
			if strings.Contains(rec.Body.String(), "test-appkey") {
				t.Errorf("応答にアプリケーションキーが含まれる: %s", rec.Body)
			}
			if tt.status != http.StatusBadGateway {
				if n := len(tt.svc.recorded()); n != 0 {
					t.Errorf("外部に %d 回問い合わせた", n)
				}
			}
		})
	}
}

// research W9: 同時に2つの検索が来ても、カーリルのセッションは1つだけ。2つ目は1つ目を待ち、30分のキャッシュを使う。
func TestConcurrentChecksShareOneCalilSession(t *testing.T) {
	svc := &fakeServices{calil: []string{contractCheckFinal}, delay: 20 * time.Millisecond}
	s := newCheckServer(t, svc, checkStart)
	s.writeData(t, contractCheckData)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rec := s.get(t, "/api/check"); rec.Code != http.StatusOK {
				t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
			}
		}()
	}
	wg.Wait()
	if n := svc.sessions(); n != 1 {
		t.Errorf("カーリルのセッション = %d, want 1", n)
	}
}

// research W9: 所蔵の検索と図書館の検索が同時に来ても、カーリルへの要求は同時に1つまで。
func TestCheckAndLibrarySearchDoNotOverlapAtCalil(t *testing.T) {
	svc := &fakeServices{calil: []string{exampleRunning, exampleFinal}, calilLibrary: `[]`, delay: 10 * time.Millisecond}
	s := newCheckServer(t, svc, checkStart)
	s.writeDataFile(t, "check_example.json")
	bare := newServer(s.cfg)

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
			req.AddCookie(s.cookie)
			rec := httptest.NewRecorder()
			bare.dispatch(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
			}
		}()
		go func() {
			defer wg.Done()
			// 図書館の検索（/api/library-search）と同じく、カーリルのロックの中で問い合わせる。
			err := bare.withCalil(func(c *calil.Client) error {
				_, err := c.Libraries(context.Background(), "東京都", "世田谷区")
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if m := svc.calilMaxInFlight.Load(); m != 1 {
		t.Errorf("カーリルへの同時の要求 = %d, want 1", m)
	}
}

// FR-103: 判定はサーバーの時間帯によらず日本時間の今日で行う。
func TestCheckUsesTokyoDateRegardlessOfLocalZone(t *testing.T) {
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })

	// 2026-10-21T00:05+09:00（水曜）= 2026-10-20T15:05Z（UTC では火曜）。
	now := time.Date(2026, 10, 20, 15, 5, 0, 0, time.UTC)
	svc := &fakeServices{calil: []string{contractCheckFinal}}
	s := newCheckServer(t, svc, now)
	s.writeData(t, `{"version": 1,
	  "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "addedOn": "2026-10-01"}],
	  "libraries": [{"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館",
	    "schedule": {"weekly": {"tue": {"closed": true}, "wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-01"},
	    "closures": []}]}`)

	var got checkJSONBody
	raw, _ := json.Marshal(s.checkJSON(t, "/api/check"))
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.CheckedAt != "2026-10-21T00:05:00+09:00" {
		t.Errorf("checkedAt = %q, want 2026-10-21T00:05:00+09:00", got.CheckedAt)
	}
	libs := append(got.Readable, got.NotReadable...)
	if len(libs) != 1 || libs[0].Opening.Status != "notYetOpen" || libs[0].Opening.Label != "開館前 09:00〜19:00" {
		t.Errorf("水曜の開館前と判定していない: %+v", libs)
	}
}

// research W9: 祝日の取得のあいだはカーリルのロックを持たない。祝日の取得が遅くても図書館の検索は待たされない。
func TestSlowHolidaysDoNotBlockCalil(t *testing.T) {
	svc := &fakeServices{
		calil:           []string{contractCheckFinal},
		calilLibrary:    `[]`,
		holidaysStarted: make(chan struct{}),
		holidaysRelease: make(chan struct{}),
	}
	s := newCheckServer(t, svc, checkStart)
	// 祝日の扱いが「休館」なので、判定に祝日の一覧を使う。
	s.writeData(t, `{"version": 1,
	  "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "addedOn": "2026-10-01"}],
	  "libraries": [{"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館",
	    "schedule": {"weekly": {"wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "closed"}, "updatedOn": "2026-10-01"},
	    "closures": []}]}`)
	bare := newServer(s.cfg)

	checked := make(chan struct{})
	go func() {
		defer close(checked)
		req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
		req.AddCookie(s.cookie)
		rec := httptest.NewRecorder()
		bare.dispatch(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
		}
	}()
	// 検索が祝日の一覧を待っているあいだに、図書館の検索をする。
	<-svc.holidaysStarted
	searched := make(chan error, 1)
	go func() {
		searched <- bare.withCalil(func(c *calil.Client) error {
			_, err := c.Libraries(context.Background(), "東京都", "")
			return err
		})
	}()
	select {
	case err := <-searched:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Error("図書館の検索が、祝日の取得が終わるまで待たされた")
	}
	close(svc.holidaysRelease)
	<-checked
}
