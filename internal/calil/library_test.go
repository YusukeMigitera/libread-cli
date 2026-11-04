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
	// 記録した応答には、世田谷区立の21館と区内の大学図書館などの14館が含まれる。
	if len(libs) != 35 {
		t.Fatalf("図書館 = %d, want 35", len(libs))
	}
	want := LibraryInfo{
		ID:      domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "上北沢図書館"},
		Formal:  "世田谷区立上北沢図書館",
		Pref:    "東京都",
		City:    "世田谷区",
		Address: "東京都世田谷区上北沢3-8-9 上北沢区民センター地階",
	}
	if libs[0] != want {
		t.Errorf("libs[0] = %+v, want %+v", libs[0], want)
	}
	ids := make(map[string]bool, len(libs))
	for _, l := range libs {
		ids[l.ID.String()] = true
	}
	for _, id := range []string{"Tokyo_Setagaya:中央図書館", "Univ_Komazawa:図書館", "Univ_Kokushikan:中央"} {
		if !ids[id] {
			t.Errorf("%s がない", id)
		}
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
		w.Write([]byte(readFixture(t, "library_system_setagaya.json")))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, AppKey: "test-key", HTTP: srv.Client(), UserAgent: "libread/test"}
	libs, err := c.LibrariesOfSystem(context.Background(), "Tokyo_Setagaya")
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 21 {
		t.Errorf("図書館 = %d, want 21", len(libs))
	}
	for _, l := range libs {
		if l.ID.SystemID != "Tokyo_Setagaya" {
			t.Errorf("別のシステムの図書館がある: %s", l.ID)
		}
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
	} {
		srv := httptest.NewServer(h)
		c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
		if _, err := c.Libraries(context.Background(), "東京都", ""); err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
		srv.Close()
	}
}

// 上流のデータに ID の欠けた館が1館あっても、他の館は使えるようにする。
func TestLibrariesSkipsEntriesWithoutID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
		  {"systemid": "", "libkey": "中央", "formal": "システムIDなし"},
		  {"systemid": "Tokyo_Setagaya", "libkey": "", "formal": "館キーなし"},
		  {"systemid": "Tokyo_Setagaya", "libkey": "中央図書館", "formal": "世田谷区立中央図書館"}]`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	libs, err := c.Libraries(context.Background(), "東京都", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].ID.String() != "Tokyo_Setagaya:中央図書館" {
		t.Errorf("libs = %+v, want ID のある1館だけ", libs)
	}
}
