package calil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"libread/internal/domain"
)

func TestLibrariesParsesResponse(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Write([]byte(readFixture(t, "library_setagaya.json")))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, AppKey: "test-key", HTTP: srv.Client(), UserAgent: "libread/test"}

	libs, err := c.Libraries(context.Background(), "東京都", "世田谷区")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/library" {
		t.Errorf("path = %s", got.URL.Path)
	}
	q := got.URL.Query()
	for k, want := range map[string]string{"appkey": "test-key", "pref": "東京都", "city": "世田谷区", "format": "json", "callback": ""} {
		if !q.Has(k) || q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if ua := got.Header.Get("User-Agent"); ua != "libread/test" {
		t.Errorf("User-Agent = %q", ua)
	}
	if len(libs) != 4 {
		t.Fatalf("図書館 = %d, want 4", len(libs))
	}
	want := LibraryInfo{
		ID:      domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "中央"},
		Formal:  "世田谷区立中央図書館",
		Pref:    "東京都",
		City:    "世田谷区",
		Address: "東京都世田谷区弦巻3-16-8",
	}
	if libs[0] != want {
		t.Errorf("libs[0] = %+v, want %+v", libs[0], want)
	}
	if libs[3].ID.String() != "Univ_Komazawa:図書館" {
		t.Errorf("libs[3].ID = %s", libs[3].ID)
	}
}

func TestLibrariesWithoutCityOmitsIt(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	libs, err := c.Libraries(context.Background(), "東京都", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 0 {
		t.Errorf("libs = %v", libs)
	}
	if got.URL.Query().Has("city") {
		t.Errorf("city を送った: %s", got.URL.RawQuery)
	}
}

func TestLibrariesRequiresPref(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:0", HTTP: http.DefaultClient}
	if _, err := c.Libraries(context.Background(), "", "世田谷区"); err == nil || !strings.Contains(err.Error(), "都道府県") {
		t.Errorf("err = %v, want 都道府県が必要", err)
	}
}

func TestLibrariesOfSystem(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Write([]byte(readFixture(t, "library_setagaya.json")))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, AppKey: "test-key", HTTP: srv.Client(), UserAgent: "libread/test"}
	if _, err := c.LibrariesOfSystem(context.Background(), "Tokyo_Setagaya"); err != nil {
		t.Fatal(err)
	}
	q := got.URL.Query()
	if q.Get("systemid") != "Tokyo_Setagaya" || q.Has("pref") || q.Get("appkey") != "test-key" {
		t.Errorf("要求 = %s", got.URL.RawQuery)
	}
}

func TestLibrariesErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"HTTP エラー": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
		"形式の誤り":    func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{")) },
		"ID の欠落": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`[{"systemid": "", "libkey": "中央", "formal": "名前"}]`))
		},
	} {
		srv := httptest.NewServer(h)
		c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
		if _, err := c.Libraries(context.Background(), "東京都", ""); err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
		srv.Close()
	}
}
