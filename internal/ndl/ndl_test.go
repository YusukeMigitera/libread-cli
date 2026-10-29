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

	got, err := newClient(srv).Search(context.Background(), "吾輩は猫である")
	if err != nil {
		t.Fatal(err)
	}
	// ISBN のない記録は除き、同じ ISBN の記録は最初の1件だけにする。10桁・ハイフン付きは13桁に正規化する。
	want := []domain.Book{
		{ISBN: mustISBN(t, "9784003101018"), Title: "吾輩は猫である", Author: "夏目漱石 著", Publisher: "岩波書店"},
		{ISBN: mustISBN(t, "9784101010014"), Title: "吾輩は猫である", Author: "夏目漱石 著", Publisher: "新潮社"},
		{ISBN: mustISBN(t, "9784041001141"), Title: "吾輩は猫である 注釈版", Author: "夏目漱石 著、注釈者 注", Publisher: "角川書店"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d 件 %+v, want %d 件", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
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
	if q.Get("title") != "吾輩は猫である" || q.Has("isbn") {
		t.Errorf("query = %s, want title だけ", r.URL.RawQuery)
	}
	if q.Get("cnt") != "10" || q.Get("dpid") != "iss-ndl-opac" {
		t.Errorf("query = %s, want cnt=10&dpid=iss-ndl-opac", r.URL.RawQuery)
	}
	if r.Header.Get("User-Agent") != testUserAgent {
		t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
	}
}

func TestSearchByISBN(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, fixture(t, "opensearch_isbn.xml"))

	// ハイフン付き・10桁で指定しても13桁で問い合わせる。
	got, err := newClient(srv).Search(context.Background(), "4-00-310101-4")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Book{ISBN: mustISBN(t, "9784003101018"), Title: "吾輩は猫である", Author: "夏目漱石 著", Publisher: "岩波書店"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}

	q := srv.recorded()[0].URL.Query()
	if q.Get("isbn") != "9784003101018" || q.Has("title") {
		t.Errorf("query = %v, want isbn=9784003101018 だけ", q)
	}
	if q.Get("cnt") != "10" || q.Get("dpid") != "iss-ndl-opac" {
		t.Errorf("query = %v, want cnt=10&dpid=iss-ndl-opac", q)
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

	got, err := newClient(srv).Search(context.Background(), "本")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Errorf("got %d 件, want 10", len(got))
	}
}

func TestSearchSkipsInvalidEntries(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, rss(
		item("9784003101019", "チェックディジット不正"),
		item("9784003101018", ""),
		`<item><dc:title>ISBN 以外の識別子だけ</dc:title><dc:identifier xsi:type="dcndl:JPNO">9784003101100</dc:identifier></item>`,
		item("9784003101100", "坊っちゃん"),
	))

	got, err := newClient(srv).Search(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ISBN != "9784003101100" {
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
			if _, err := newClient(srv).Search(context.Background(), "x"); err == nil {
				t.Error("err = nil, want error")
			}
		})
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	srv := newFakeServer(t, http.StatusOK, rss())
	if _, err := newClient(srv).Search(context.Background(), " "); err == nil {
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
