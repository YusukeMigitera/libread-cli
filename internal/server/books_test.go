package server

import (
	"encoding/json"
	"fmt"
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

// 読みかけの本（contracts/http-api.md「読みかけの本」）のテスト。NDL サーチは記録済みの応答（internal/ndl/testdata）で差し替える。

// fakeNDL は NDL サーチの代わりに応答する。受けた要求を記録する。
type fakeNDL struct {
	mu       sync.Mutex
	requests []recordedNDLRequest
	// respond は要求のクエリーから応答を決める。nil なら ndlFixtureFor を使う。
	respond func(q url.Values) (int, string)
}

type recordedNDLRequest struct {
	host   string
	url    string
	header http.Header
}

func (f *fakeNDL) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, recordedNDLRequest{host: r.Host, url: r.URL.String(), header: r.Header.Clone()})
	respond := f.respond
	f.mu.Unlock()
	if respond == nil {
		respond = ndlFixtureFor
	}
	status, body := respond(r.URL.Query())
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func (f *fakeNDL) recorded() []recordedNDLRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedNDLRequest(nil), f.requests...)
}

// ndlFixtureFor は、001 の記録済みの応答のうち、クエリーに合うものを返す。
func ndlFixtureFor(q url.Values) (int, string) {
	name := "opensearch_title.xml"
	switch {
	case q.Has("isbn"):
		name = "opensearch_isbn.xml"
	case q.Has("creator") || q.Has("publisher"):
		name = "opensearch_title_author_publisher.xml"
	}
	raw, err := os.ReadFile(filepath.Join("..", "ndl", "testdata", name))
	if err != nil {
		return http.StatusInternalServerError, err.Error()
	}
	return http.StatusOK, string(raw)
}

// ndlFeed は items を持つ OpenSearch の応答を作る。
func ndlFeed(total int, items ...string) string {
	return `<rss xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:openSearch="http://a9.com/-/spec/opensearchrss/1.0/" ` +
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" version="2.0"><channel>` +
		fmt.Sprintf("<openSearch:totalResults>%d</openSearch:totalResults>", total) +
		strings.Join(items, "") + `</channel></rss>`
}

// bookServer は本の API のテスト用のサーバー。NDL サーチへの要求は ndl に届く。
type bookServer struct {
	*testServer
	ndl    *fakeNDL
	cookie *http.Cookie
}

func newBookServer(t *testing.T, mutate ...func(*Config)) *bookServer {
	t.Helper()
	ndl := &fakeNDL{}
	srv := httptest.NewServer(ndl)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	all := append([]func(*Config){func(c *Config) {
		c.HTTP = &http.Client{Transport: routeTransport{target: target}}
	}}, mutate...)
	ts := newTestServer(t, all...)
	return &bookServer{testServer: ts, ndl: ndl, cookie: ts.sessionCookie(t)}
}

func (s *bookServer) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, http.MethodGet, target, "", withCookie(s.cookie))
}

func (s *bookServer) send(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, method, target, body, withCookie(s.cookie), withOrigin(testOrigin))
}

// books は保存されている読みかけの本（GET /api/books）。
func (s *bookServer) books(t *testing.T) []bookJSON {
	t.Helper()
	var out struct{ Books []bookJSON }
	s.getJSON(t, "/api/books", &out)
	return out.Books
}

func errorCodeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return (&testServerResponse{status: rec.Code, body: rec.Body.Bytes()}).errorCode(t)
}

// twoBooksData は読みかけの本が2冊のデータ。赤い実は書名での検索結果（opensearch_title.xml）の1件目。
const twoBooksData = `{"version": 1,
  "books": [
    {"isbn": "9784895283694", "title": "赤い実 : ほか", "note": "p.10まで", "addedOn": "2026-10-01"},
    {"isbn": "9784003101100", "title": "坊っちゃん", "author": "夏目漱石", "addedOn": "2026-10-02"}],
  "libraries": []}`

type bookSearchBody struct {
	Books []struct {
		ISBN       string `json:"isbn"`
		Title      string `json:"title"`
		Author     string `json:"author"`
		Publisher  string `json:"publisher"`
		Registered bool   `json:"registered"`
	} `json:"books"`
	Total     int     `json:"total"`
	Truncated bool    `json:"truncated"`
	Hint      *string `json:"hint"`
	Credit    struct {
		Label   string `json:"label"`
		URL     string `json:"url"`
		License struct {
			Label string `json:"label"`
			URL   string `json:"url"`
		} `json:"license"`
	} `json:"credit"`
}

func (s *bookServer) search(t *testing.T, target string) bookSearchBody {
	t.Helper()
	rec := s.get(t, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body = %s", target, rec.Code, rec.Body)
	}
	var out bookSearchBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

const narrowingHintWeb = "表示しきれていない本があります。見つからないときは著者や出版社で絞り込むか、ISBN で探してください"

func TestBookSearchByTitle(t *testing.T) {
	s := newBookServer(t)
	s.writeData(t, twoBooksData)

	rec := s.get(t, "/api/book-search?q="+url.QueryEscape("吾輩は猫である"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertMatchesExample(t, "book-search", rec.Body.Bytes())
	var got bookSearchBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	// 並べ替えと件数は 001 の book find と同じ（internal/ndl）。
	if len(got.Books) != 10 || got.Total != 345 || !got.Truncated {
		t.Fatalf("books = %d 件, total = %d, truncated = %v, want 10, 345, true", len(got.Books), got.Total, got.Truncated)
	}
	first := got.Books[0]
	if first.ISBN != "9784895283694" || first.Title != "赤い実 : ほか" || first.Publisher != "光村図書出版" || !first.Registered {
		t.Errorf("books[0] = %+v, want 登録済みの赤い実", first)
	}
	for _, b := range got.Books[1:] {
		if b.Registered {
			t.Errorf("%s は登録していない: %+v", b.ISBN, b)
		}
	}
	if got.Hint == nil || *got.Hint != narrowingHintWeb {
		t.Errorf("hint = %v, want %q", got.Hint, narrowingHintWeb)
	}
	c := got.Credit
	if c.Label != "書誌情報: 国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータを利用しています" ||
		c.URL != "https://ndlsearch.ndl.go.jp/" ||
		c.License.Label != "クリエイティブ・コモンズ 表示 4.0 国際" ||
		c.License.URL != "https://creativecommons.org/licenses/by/4.0/legalcode.ja" {
		t.Errorf("credit = %+v", c)
	}

	reqs := s.ndl.recorded()
	if len(reqs) != 1 {
		t.Fatalf("NDL サーチへの要求 = %d, want 1", len(reqs))
	}
	if reqs[0].host != "ndlsearch.ndl.go.jp" || !strings.Contains(reqs[0].url, "title=") {
		t.Errorf("要求 = %s %s, want 書名での検索", reqs[0].host, reqs[0].url)
	}
	if ua := reqs[0].header.Get("User-Agent"); !strings.HasPrefix(ua, "libread/") {
		t.Errorf("User-Agent = %q", ua)
	}
}

func TestBookSearchHint(t *testing.T) {
	noISBN := `<item><dc:title>吾輩は猫である（ISBN なし）</dc:title></item>`
	tests := []struct {
		name     string
		query    string
		respond  func(url.Values) (int, string)
		books    int
		wantHint string // 空なら hint を付けない
	}{
		// 著者・出版社で絞り込んで、すべて表示できた。
		{name: "表示しきれた", query: "q=" + url.QueryEscape("吾輩は猫である") + "&author=" + url.QueryEscape("夏目漱石") + "&publisher=" + url.QueryEscape("岩波書店"), books: 6},
		// 一致はあるが、取得した中に ISBN のある本がない（001 と同じ条件）。
		{name: "ISBN のある本がない", query: "q=" + url.QueryEscape("吾輩は猫である"),
			respond:  func(url.Values) (int, string) { return http.StatusOK, ndlFeed(50, noISBN) },
			wantHint: "50件の一致がありますが、取得した中に ISBN のある本がありませんでした。著者や出版社で絞り込むか、ISBN で探してください"},
		{name: "一致なし", query: "q=" + url.QueryEscape("存在しない本"),
			respond: func(url.Values) (int, string) { return http.StatusOK, ndlFeed(0) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newBookServer(t)
			s.ndl.respond = tt.respond
			rec := s.get(t, "/api/book-search?"+tt.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			var raw map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			books, ok := raw["books"].([]any)
			if !ok || len(books) != tt.books {
				t.Errorf("books = %v, want %d 件（空なら []）", raw["books"], tt.books)
			}
			hint, has := raw["hint"]
			switch {
			case tt.wantHint == "" && has:
				t.Errorf("hint = %v, want なし", hint)
			case tt.wantHint != "" && hint != tt.wantHint:
				t.Errorf("hint = %v, want %q", hint, tt.wantHint)
			}
		})
	}
}

func TestBookSearchPassesFilters(t *testing.T) {
	s := newBookServer(t)
	s.search(t, "/api/book-search?q="+url.QueryEscape(" 吾輩は猫である ")+"&author="+url.QueryEscape("夏目漱石")+"&publisher="+url.QueryEscape("岩波書店"))
	reqs := s.ndl.recorded()
	if len(reqs) != 1 {
		t.Fatalf("NDL サーチへの要求 = %d, want 1", len(reqs))
	}
	u, err := url.Parse(reqs[0].url)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("title") != "吾輩は猫である" || q.Get("creator") != "夏目漱石" || q.Get("publisher") != "岩波書店" {
		t.Errorf("query = %v", q)
	}
}

func TestBookSearchByISBNMarksRegistered(t *testing.T) {
	s := newBookServer(t)
	s.writeData(t, `{"version": 1, "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "addedOn": "2026-10-01"}], "libraries": []}`)
	got := s.search(t, "/api/book-search?q=4-00-310101-4")
	if len(got.Books) != 1 || got.Books[0].ISBN != "9784003101018" || !got.Books[0].Registered || got.Truncated || got.Hint != nil {
		t.Errorf("got %+v", got)
	}
}

func TestBookSearchErrors(t *testing.T) {
	for _, target := range []string{"/api/book-search", "/api/book-search?q=", "/api/book-search?q=%20%20&author=x"} {
		s := newBookServer(t)
		rec := s.get(t, target)
		if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec) != "invalid" {
			t.Errorf("GET %s: status = %d, body = %s, want 400 invalid", target, rec.Code, rec.Body)
		}
		if n := len(s.ndl.recorded()); n != 0 {
			t.Errorf("GET %s: NDL サーチへの要求 = %d, want 0", target, n)
		}
	}

	s := newBookServer(t)
	s.ndl.respond = func(url.Values) (int, string) { return http.StatusServiceUnavailable, "" }
	rec := s.get(t, "/api/book-search?q=x")
	if rec.Code != http.StatusBadGateway || errorCodeOf(t, rec) != "upstream" {
		t.Errorf("NDL サーチが 503: status = %d, body = %s, want 502 upstream", rec.Code, rec.Body)
	}
}

// research W11: 書名の検索語はログに書かない。NDL サーチに接続できないときのエラーにも、要求の URL（検索語）を含めない。
func TestBookSearchUnreachableDoesNotLogQuery(t *testing.T) {
	s := newBookServer(t, func(c *Config) {
		c.HTTP = &http.Client{Transport: routeTransport{target: &url.URL{Scheme: "http", Host: "127.0.0.1:1"}}}
	})
	rec := s.get(t, "/api/book-search?q="+url.QueryEscape("秘密の書名"))
	if rec.Code != http.StatusBadGateway || errorCodeOf(t, rec) != "upstream" {
		t.Fatalf("status = %d, body = %s, want 502 upstream", rec.Code, rec.Body)
	}
	for _, leak := range []string{"秘密の書名", url.QueryEscape("秘密の書名")} {
		if strings.Contains(s.log.String(), leak) || strings.Contains(rec.Body.String(), leak) {
			t.Errorf("検索語 %q がログか応答に含まれる:\nlog: %s\nbody: %s", leak, s.log, rec.Body)
		}
	}
}

func TestAddBook(t *testing.T) {
	// 2026-11-05 15:30 UTC は日本時間で 11月6日 00:30。登録日は日本時間の今日。
	now := time.Date(2026, 11, 5, 15, 30, 0, 0, time.UTC)
	s := newBookServer(t, func(c *Config) { c.Now = func() time.Time { return now } })
	s.writeData(t, twoBooksData)

	rec := s.send(t, http.MethodPost, "/api/books", `{"isbn": "4-00-310101-4", "note": "p.120まで"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got bookJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := bookJSON{ISBN: "9784003101018", Title: "吾輩は猫である", Author: "夏目, 漱石, 1867-1916", Publisher: "岩波書店", Note: "p.120まで", AddedOn: "2026-11-06"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	assertMatchesExample(t, "books", []byte(`{"books": [`+rec.Body.String()+`]}`))

	// 登録順の最後に加わる。
	books := s.books(t)
	if len(books) != 3 || books[0].ISBN != "9784895283694" || books[2] != want {
		t.Errorf("books = %+v", books)
	}
	reqs := s.ndl.recorded()
	if len(reqs) != 1 || !strings.Contains(reqs[0].url, "isbn=9784003101018") {
		t.Errorf("NDL サーチへの要求 = %+v, want ISBN での1回", reqs)
	}
}

func TestAddBookWithoutNote(t *testing.T) {
	s := newBookServer(t)
	rec := s.send(t, http.MethodPost, "/api/books", `{"isbn": "9784003101018"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if books := s.books(t); len(books) != 1 || books[0].Note != "" || books[0].AddedOn != "2026-11-05" {
		t.Errorf("books = %+v", books)
	}
}

func TestAddBookErrors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		respond  func(url.Values) (int, string)
		status   int
		code     string
		requests int
	}{
		{name: "登録済み", body: `{"isbn": "978-4-89528-369-4"}`, status: http.StatusConflict, code: "conflict"},
		{name: "チェックディジットが不正", body: `{"isbn": "9784003101019"}`, status: http.StatusBadRequest, code: "invalid"},
		{name: "ISBN なし", body: `{"note": "x"}`, status: http.StatusBadRequest, code: "invalid"},
		{name: "JSON でない", body: `isbn=9784003101018`, status: http.StatusBadRequest, code: "invalid"},
		{name: "メモが文字列でない", body: `{"isbn": "9784003101018", "note": 1}`, status: http.StatusBadRequest, code: "invalid"},
		{name: "NDL サーチに書誌がない", body: `{"isbn": "9784003101018"}`,
			respond: func(url.Values) (int, string) { return http.StatusOK, ndlFeed(0) },
			status:  http.StatusBadRequest, code: "invalid", requests: 1},
		{name: "NDL サーチに接続できない", body: `{"isbn": "9784003101018"}`,
			respond: func(url.Values) (int, string) { return http.StatusInternalServerError, "" },
			status:  http.StatusBadGateway, code: "upstream", requests: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newBookServer(t)
			s.ndl.respond = tt.respond
			s.writeData(t, twoBooksData)
			before := readDataFile(t, s.cfg.DataDir)

			rec := s.send(t, http.MethodPost, "/api/books", tt.body)
			if rec.Code != tt.status || errorCodeOf(t, rec) != tt.code {
				t.Errorf("status = %d, body = %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
			if n := len(s.ndl.recorded()); n != tt.requests {
				t.Errorf("NDL サーチへの要求 = %d, want %d", n, tt.requests)
			}
			if after := readDataFile(t, s.cfg.DataDir); after != before {
				t.Errorf("保存した:\n%s", after)
			}
		})
	}
}

func TestUpdateNote(t *testing.T) {
	s := newBookServer(t)
	s.writeData(t, twoBooksData)

	rec := s.send(t, http.MethodPatch, "/api/books/9784003101100", `{"note": "p.180まで"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got bookJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := bookJSON{ISBN: "9784003101100", Title: "坊っちゃん", Author: "夏目漱石", Note: "p.180まで", AddedOn: "2026-10-02"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if books := s.books(t); books[1] != want || books[0].Note != "p.10まで" {
		t.Errorf("books = %+v", books)
	}

	// 空文字ならメモを消す。ISBN-10 やハイフン付きでも指定できる。
	rec = s.send(t, http.MethodPatch, "/api/books/4-89528-369-0", `{"note": ""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("消去: status = %d, body = %s", rec.Code, rec.Body)
	}
	if books := s.books(t); books[0].Note != "" || books[1].Note != "p.180まで" {
		t.Errorf("books = %+v", books)
	}
	if n := len(s.ndl.recorded()); n != 0 {
		t.Errorf("NDL サーチへの要求 = %d, want 0", n)
	}
}

func TestUpdateNoteErrors(t *testing.T) {
	tests := []struct {
		name, target, body string
		status             int
		code               string
	}{
		{name: "登録していない", target: "/api/books/9784003101018", body: `{"note": "x"}`, status: http.StatusNotFound, code: "notFound"},
		{name: "ISBN が不正", target: "/api/books/123", body: `{"note": "x"}`, status: http.StatusBadRequest, code: "invalid"},
		{name: "note がない", target: "/api/books/9784003101100", body: `{}`, status: http.StatusBadRequest, code: "invalid"},
		{name: "JSON でない", target: "/api/books/9784003101100", body: `note`, status: http.StatusBadRequest, code: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newBookServer(t)
			s.writeData(t, twoBooksData)
			before := readDataFile(t, s.cfg.DataDir)
			rec := s.send(t, http.MethodPatch, tt.target, tt.body)
			if rec.Code != tt.status || errorCodeOf(t, rec) != tt.code {
				t.Errorf("status = %d, body = %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
			if after := readDataFile(t, s.cfg.DataDir); after != before {
				t.Errorf("保存した:\n%s", after)
			}
		})
	}
}

func TestRemoveBook(t *testing.T) {
	s := newBookServer(t)
	s.writeData(t, twoBooksData)

	rec := s.send(t, http.MethodDelete, "/api/books/9784895283694", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q, want 204 本文なし", rec.Code, rec.Body)
	}
	if books := s.books(t); len(books) != 1 || books[0].ISBN != "9784003101100" {
		t.Errorf("books = %+v", books)
	}

	rec = s.send(t, http.MethodDelete, "/api/books/9784895283694", "")
	if rec.Code != http.StatusNotFound || errorCodeOf(t, rec) != "notFound" {
		t.Errorf("もう一度: status = %d, body = %s, want 404 notFound", rec.Code, rec.Body)
	}
	rec = s.send(t, http.MethodDelete, "/api/books/abc", "")
	if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec) != "invalid" {
		t.Errorf("ISBN が不正: status = %d, body = %s, want 400 invalid", rec.Code, rec.Body)
	}
}

// spec Edge Cases・research W3: 2つの端末からほぼ同時に別の本のメモを保存しても、どちらも残る。
func TestConcurrentNoteUpdatesOnDifferentBooks(t *testing.T) {
	s := newBookServer(t)
	s.writeData(t, twoBooksData)

	const rounds = 20
	var wg sync.WaitGroup
	for _, isbn := range []string{"9784895283694", "9784003101100"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range rounds {
				req := httptest.NewRequest(http.MethodPatch, "/api/books/"+isbn, strings.NewReader(fmt.Sprintf(`{"note": "%s-%d"}`, isbn, i)))
				req.AddCookie(s.cookie)
				req.Header.Set("Origin", testOrigin)
				rec := httptest.NewRecorder()
				s.handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
				}
			}
		}()
	}
	wg.Wait()

	books := s.books(t)
	if len(books) != 2 {
		t.Fatalf("books = %+v", books)
	}
	for _, b := range books {
		if want := fmt.Sprintf("%s-%d", b.ISBN, rounds-1); b.Note != want {
			t.Errorf("%s のメモ = %q, want %q", b.ISBN, b.Note, want)
		}
	}
}

// FR-114: メモは外部サービスに送らない。ログにも書かない（research W11）。
func TestNoteIsNeverSentOutside(t *testing.T) {
	const note = "ひみつのメモ p.42"
	s := newBookServer(t)

	steps := []struct{ method, target, body string }{
		{http.MethodPost, "/api/books", `{"isbn": "9784003101018", "note": "` + note + `"}`},
		{http.MethodPatch, "/api/books/9784003101018", `{"note": "` + note + `（続き）"}`},
		{http.MethodGet, "/api/book-search?q=" + url.QueryEscape("吾輩は猫である"), ""},
		{http.MethodGet, "/api/books", ""},
	}
	for _, st := range steps {
		if rec := s.send(t, st.method, st.target, st.body); rec.Code >= 300 {
			t.Fatalf("%s %s: status = %d, body = %s", st.method, st.target, rec.Code, rec.Body)
		}
	}
	reqs := s.ndl.recorded()
	if len(reqs) != 2 {
		t.Errorf("NDL サーチへの要求 = %d, want 2（登録と検索）", len(reqs))
	}
	for _, r := range reqs {
		raw := r.url
		for k, v := range r.header {
			raw += k + strings.Join(v, ",")
		}
		for _, leak := range []string{note, url.QueryEscape(note), "ひみつ"} {
			if strings.Contains(raw, leak) {
				t.Errorf("メモが外部への要求に含まれる: %s", raw)
			}
		}
	}
	if strings.Contains(s.log.String(), "ひみつ") {
		t.Errorf("メモがログに含まれる: %s", s.log)
	}
}

// research W9: 書誌検索と本の登録は、NDL サーチへの要求を同時に1つまでにする。
func TestBookSearchAndAddDoNotOverlapAtNDL(t *testing.T) {
	s := newBookServer(t)
	var inFlight, maxInFlight int
	var mu sync.Mutex
	s.ndl.respond = func(q url.Values) (int, string) {
		mu.Lock()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return ndlFixtureFor(q)
	}

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var req *http.Request
			if i%2 == 0 {
				req = httptest.NewRequest(http.MethodGet, "/api/book-search?q=x", nil)
			} else {
				req = httptest.NewRequest(http.MethodPost, "/api/books", strings.NewReader(`{"isbn": "9784003101018"}`))
				req.Header.Set("Origin", testOrigin)
			}
			req.AddCookie(s.cookie)
			rec := httptest.NewRecorder()
			s.handler.ServeHTTP(rec, req)
			if rec.Code >= 500 {
				t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
			}
		}()
	}
	wg.Wait()
	if maxInFlight != 1 {
		t.Errorf("NDL サーチへの同時の要求 = %d, want 1", maxInFlight)
	}
	// 同じ本の登録が重なっても、1冊だけ登録する。
	if books := s.books(t); len(books) != 1 {
		t.Errorf("books = %+v, want 1冊", books)
	}
}
