package ndl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"libread/internal/domain"
)

const testUserAgent = "libread/test"

// fixture は testdata に記録した NDL サーチ OpenSearch の実際の応答（2026-10-29 取得）。
//   - opensearch_title.xml: GET /api/opensearch?title=吾輩は猫である&cnt=20&dpid=iss-ndl-opac
//   - opensearch_title_author_publisher.xml:
//     GET /api/opensearch?title=吾輩は猫である&creator=夏目漱石&publisher=岩波書店&cnt=20&dpid=iss-ndl-opac
//   - opensearch_isbn.xml:  GET /api/opensearch?isbn=9784003101018&cnt=20&dpid=iss-ndl-opac
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func (f *fakeServer) recorded() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.requests...)
}

func newFakeServer(t *testing.T, status int, body string) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r)
		f.mu.Unlock()
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func newClient(srv *fakeServer) *Client {
	return &Client{BaseURL: srv.URL + "/api/opensearch", HTTP: srv.Client(), UserAgent: testUserAgent}
}

func mustISBN(t *testing.T, s string) domain.ISBN {
	t.Helper()
	i, err := domain.ParseISBN(s)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestSearchByTitle(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, fixture(t, "opensearch_title.xml"))

	got, err := newClient(srv).Search(context.Background(), Query{Text: "吾輩は猫である"})
	if err != nil {
		t.Fatal(err)
	}
	// 応答の20件は書名の読みの五十音順で、書名が「吾輩は猫である」と一致する・それで始まる本はない。
	// ISBN のない8件を除いた12件のうち、NDL の順で上位10件を返す。
	// 10桁・ハイフン付きの ISBN は13桁に正規化し、著者・出版社が複数あれば「、」でつなぐ。
	// 著者は dc:creator（典拠形「姓, 名, 生没年」）をそのまま使う。dc:creator のない記録は著者が空になる。
	want := []domain.Book{
		{ISBN: mustISBN(t, "9784895283694"), Title: "赤い実 : ほか", Author: "井上, 靖, 1907-1991、三木, 卓, 1935-2023、安岡, 章太郎, 1920-2013、直野, 敦, 1929-", Publisher: "光村図書出版"},
		{ISBN: mustISBN(t, "9784072966969"), Title: "頭のいい子を育てる日本のおはなし : ハンディタイプ : おでかけに最適!軽くて持ち運びやすい", Author: "主婦の友社", Publisher: "主婦の友社"},
		{ISBN: mustISBN(t, "9784795292833"), Title: "エリザベス : 現代版「吾輩は猫である」", Author: "山川, とおる, 1946-", Publisher: "東京図書出版会、星雲社 (発売)"},
		{ISBN: mustISBN(t, "9784790715917"), Title: "男漱石を女が読む", Author: "渡辺, 澄子, 1930- 近代日本文学", Publisher: "世界思想社"},
		{ISBN: mustISBN(t, "9784784511396"), Title: "革命伝説・宮本研の劇世界", Author: "日本演劇学会", Publisher: "社会評論社"},
		{ISBN: mustISBN(t, "9784405072077"), Title: "考える力を伸ばす!心を育てる!読み聞かせ366話", Author: "新星出版社", Publisher: "新星出版社"},
		{ISBN: mustISBN(t, "9787559638427"), Title: "我是猫", Author: "夏目, 漱石, 1867-1916、孙, 静", Publisher: "北京联合出版"},
		{ISBN: mustISBN(t, "9789888570096"), Title: "我是貓 : 首二章 : 日漢對照・精裝有聲版", Author: "夏目, 漱石, 1867-1916、鄒, 波", Publisher: "香港中和出版、香港聯合書刊物流"},
		{ISBN: mustISBN(t, "9787806572856"), Title: "我是猫", Author: "", Publisher: "译林出版社"},
		{ISBN: mustISBN(t, "4-01-061402-1"), Title: "贋作吾輩は猫である", Author: "内田, 百間, 1889-1971", Publisher: "旺文社"},
	}
	if len(got.Books) != len(want) {
		t.Fatalf("got %d 件 %+v, want %d 件", len(got.Books), got.Books, len(want))
	}
	for i := range want {
		if got.Books[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got.Books[i], want[i])
		}
	}
	// 一致は345件あり、取得した20件の先にも続くので、表示しきれていない。
	if got.Total != 345 || !got.Truncated {
		t.Errorf("Total = %d, Truncated = %v, want 345, true", got.Total, got.Truncated)
	}

	reqs := srv.recorded()
	if len(reqs) != 1 {
		t.Fatalf("要求 = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if r.URL.Path != "/api/opensearch" {
		t.Errorf("path = %s", r.URL.Path)
	}
	q := r.URL.Query()
	if q.Get("title") != "吾輩は猫である" || q.Has("isbn") || q.Has("creator") || q.Has("publisher") {
		t.Errorf("query = %s, want title だけ", r.URL.RawQuery)
	}
	if q.Get("cnt") != "20" || q.Get("dpid") != "iss-ndl-opac" {
		t.Errorf("query = %s, want cnt=20&dpid=iss-ndl-opac", r.URL.RawQuery)
	}
	if r.Header.Get("User-Agent") != testUserAgent {
		t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
	}
}

// 著者・出版社で絞ると、書名が検索語と一致する本が先頭に来る（research.md R3）。
func TestSearchByTitleAuthorPublisher(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, fixture(t, "opensearch_title_author_publisher.xml"))

	got, err := newClient(srv).Search(context.Background(), Query{Text: "吾輩は猫である", Author: "夏目漱石", Publisher: "岩波書店"})
	if err != nil {
		t.Fatal(err)
	}
	// 応答の15件（ISBN のあるものは6件）は『漱石全集』などが先で、『吾輩は猫である』は末尾にある。
	wantISBNs := []domain.ISBN{
		mustISBN(t, "4-00-310101-4"), // 吾輩は猫である（完全一致。NDL の順を保つ）
		mustISBN(t, "4-00-007215-3"), // 吾輩は猫である
		mustISBN(t, "4-00-091801-X"), // 漱石全集
		mustISBN(t, "4-00-009001-1"), // 漱石文学作品集
		mustISBN(t, "978-4-00-092821-2"),
		mustISBN(t, "978-4-00-092836-6"),
	}
	if len(got.Books) != len(wantISBNs) {
		t.Fatalf("got %d 件 %+v, want %d 件", len(got.Books), got.Books, len(wantISBNs))
	}
	for i, w := range wantISBNs {
		if got.Books[i].ISBN != w {
			t.Errorf("[%d] got %s 『%s』, want %s", i, got.Books[i].ISBN, got.Books[i].Title, w)
		}
	}
	if got.Books[0].Title != "吾輩は猫である" || got.Books[0].Publisher != "岩波書店" {
		t.Errorf("[0] = %+v", got.Books[0])
	}
	// 一致した15件をすべて取得できたので、表示しきれていない本はない。
	if got.Total != 15 || got.Truncated {
		t.Errorf("Total = %d, Truncated = %v, want 15, false", got.Total, got.Truncated)
	}

	q := srv.recorded()[0].URL.Query()
	if q.Get("title") != "吾輩は猫である" || q.Get("creator") != "夏目漱石" || q.Get("publisher") != "岩波書店" {
		t.Errorf("query = %v", q)
	}
	if q.Get("cnt") != "20" || q.Get("dpid") != "iss-ndl-opac" {
		t.Errorf("query = %v, want cnt=20&dpid=iss-ndl-opac", q)
	}
}

func TestSearchByISBN(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, fixture(t, "opensearch_isbn.xml"))

	// ハイフン付き・10桁で指定しても13桁で問い合わせる。
	got, err := newClient(srv).Search(context.Background(), Query{Text: "4-00-310101-4"})
	if err != nil {
		t.Fatal(err)
	}
	// dcndl:ISBN と dcndl:ISBN13 の両方に10桁の値が入っている。dcndl:ISBN の値を13桁に正規化して使う。
	want := domain.Book{ISBN: mustISBN(t, "9784003101018"), Title: "吾輩は猫である", Author: "夏目, 漱石, 1867-1916", Publisher: "岩波書店"}
	if len(got.Books) != 1 || got.Books[0] != want {
		t.Fatalf("got %+v, want [%+v]", got.Books, want)
	}
	if got.Total != 1 || got.Truncated {
		t.Errorf("Total = %d, Truncated = %v, want 1, false", got.Total, got.Truncated)
	}

	q := srv.recorded()[0].URL.Query()
	if q.Get("isbn") != "9784003101018" || q.Has("title") {
		t.Errorf("query = %v, want isbn=9784003101018 だけ", q)
	}
	if q.Get("cnt") != "20" || q.Get("dpid") != "iss-ndl-opac" {
		t.Errorf("query = %v, want cnt=20&dpid=iss-ndl-opac", q)
	}
}

// item は opensearch の item 要素1件。
func item(isbn, title string) string {
	return fmt.Sprintf(`<item><dc:title>%s</dc:title><dc:creator>著者</dc:creator><dc:publisher>出版社</dc:publisher>`+
		`<dc:identifier xsi:type="dcndl:ISBN">%s</dc:identifier></item>`, title, isbn)
}

func rss(items ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<rss xmlns:dcndl="http://ndl.go.jp/dcndl/terms/" xmlns:dc="http://purl.org/dc/elements/1.1/" ` +
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" version="2.0"><channel>` +
		strings.Join(items, "") + `</channel></rss>`
}

func TestSearchReturnsAtMostTen(t *testing.T) {
	var items []string
	for i := range 12 {
		body := fmt.Sprintf("978400310%03d", i)
		isbn := body + string(isbn13Check(body))
		items = append(items, item(isbn, fmt.Sprintf("本%d", i)))
	}
	srv := newFakeServer(t, http.StatusOK, rss(items...))

	got, err := newClient(srv).Search(context.Background(), Query{Text: "本"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Books) != 10 || !got.Truncated {
		t.Errorf("got %d 件, Truncated = %v, want 10 件, true", len(got.Books), got.Truncated)
	}
}

// 書名が検索語と完全一致 → 検索語で始まる → その他の順に並べ、同じ順位の中は NDL の順を保つ。
func TestSearchRanksByTitleMatch(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, rss(
		item("9784003101100", "坊っちゃん全集"),
		item("9784003101018", "注解 坊っちゃん"),
		item("9784101010014", "坊っちゃん"),
		item("9784041001141", "坊っちゃん : 新装版"),
		item("9784334051259", "坊っちゃん"),
	))

	got, err := newClient(srv).Search(context.Background(), Query{Text: " 坊っちゃん "})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.ISBN{"9784101010014", "9784334051259", "9784003101100", "9784041001141", "9784003101018"}
	if len(got.Books) != len(want) {
		t.Fatalf("got %d 件, want %d 件", len(got.Books), len(want))
	}
	for i, w := range want {
		if got.Books[i].ISBN != w {
			t.Errorf("[%d] got %s 『%s』, want %s", i, got.Books[i].ISBN, got.Books[i].Title, w)
		}
	}
	// totalResults のない応答は、取得した件数を一致件数とする。
	if got.Total != 5 || got.Truncated {
		t.Errorf("Total = %d, Truncated = %v, want 5, false", got.Total, got.Truncated)
	}
}

func TestSearchSkipsInvalidEntries(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, rss(
		item("9784003101019", "チェックディジット不正"),
		item("9784003101018", ""),
		`<item><dc:title>ISBN 以外の識別子だけ</dc:title><dc:identifier xsi:type="dcndl:JPNO">9784003101100</dc:identifier></item>`,
		item("9784003101100", "坊っちゃん"),
	))

	got, err := newClient(srv).Search(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Books) != 1 || got.Books[0].ISBN != "9784003101100" {
		t.Errorf("got %+v, want 坊っちゃん だけ", got)
	}
}

func TestSearchErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "HTTPエラー", status: http.StatusServiceUnavailable, body: ""},
		{name: "XMLが壊れている", status: http.StatusOK, body: "<rss><channel><item>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeServer(t, tt.status, tt.body)
			if _, err := newClient(srv).Search(context.Background(), Query{Text: "x"}); err == nil {
				t.Error("err = nil, want error")
			}
		})
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, rss())
	if _, err := newClient(srv).Search(context.Background(), Query{Text: " ", Author: "夏目漱石"}); err == nil {
		t.Error("err = nil, want error")
	}
	if n := len(srv.recorded()); n != 0 {
		t.Errorf("要求 = %d, want 0", n)
	}
}

func TestDefaultBaseURL(t *testing.T) {
	u, err := url.Parse(DefaultBaseURL)
	if err != nil || u.Host != "ndlsearch.ndl.go.jp" || u.Path != "/api/opensearch" {
		t.Errorf("DefaultBaseURL = %s", DefaultBaseURL)
	}
}

func isbn13Check(first12 string) byte {
	sum := 0
	for i := range 12 {
		d := int(first12[i] - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10)
}
