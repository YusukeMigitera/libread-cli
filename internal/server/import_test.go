package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"libread/internal/store"
)

func (s *testServer) postImport(t *testing.T, body string) *testServerResponse {
	t.Helper()
	rec := s.do(t, http.MethodPost, "/api/import", body, withCookie(s.sessionCookie(t)), withOrigin(testOrigin))
	return &testServerResponse{status: rec.Code, body: rec.Body.Bytes()}
}

type testServerResponse struct {
	status int
	body   []byte
}

func (r *testServerResponse) errorCode(t *testing.T) string {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("エラーの形でない: %s", r.body)
	}
	if e.Error.Message == "" {
		t.Errorf("message が空: %s", r.body)
	}
	return e.Error.Code
}

// readDataFile は保存された data.json の中身。なければ空。
func readDataFile(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "data.json"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// FR-118・SC-108: 001 の data.json をそのまま取り込める。
func TestImportSavesCLIData(t *testing.T) {
	s := newTestServer(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "check_example.json"))
	if err != nil {
		t.Fatal(err)
	}
	res := s.postImport(t, string(raw))
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.status, res.body)
	}
	assertMatchesExample(t, "import", res.body)
	var got struct{ Books, Libraries, Closures int }
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Books != 2 || got.Libraries != 6 || got.Closures != 1 {
		t.Errorf("件数 = %+v, want 本 2・図書館 6・臨時休館 1", got)
	}

	want, err := store.ParseData(raw)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load(s.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("保存した内容が取り込んだ内容と違う\n got: %+v\nwant: %+v", saved, want)
	}
}

// 空の data.json（本も図書館もない）が置いてあっても取り込める。
func TestImportIntoEmptyData(t *testing.T) {
	s := newTestServer(t)
	s.writeData(t, `{"version": 1, "books": [], "libraries": []}`)
	if res := s.postImport(t, sampleData); res.status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.status, res.body)
	}
}

func TestImportConflictsWithExistingData(t *testing.T) {
	tests := map[string]string{
		"本がある":   `{"version": 1, "books": [{"isbn": "9784003101100", "title": "Q", "addedOn": "2026-10-01"}], "libraries": []}`,
		"図書館がある": `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "N", "closures": []}]}`,
	}
	for name, existing := range tests {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t)
			s.writeData(t, existing)
			res := s.postImport(t, sampleData)
			if res.status != http.StatusConflict {
				t.Fatalf("status = %d, want 409, body = %s", res.status, res.body)
			}
			if code := res.errorCode(t); code != "conflict" {
				t.Errorf("code = %q", code)
			}
			if got := readDataFile(t, s.cfg.DataDir); got != existing {
				t.Errorf("data.json が変わった: %s", got)
			}
		})
	}
}

func TestImportRejectsInvalidContent(t *testing.T) {
	tests := map[string]string{
		"JSON でない":     "これは JSON ではない",
		"バージョンが違う":     `{"version": 2, "books": [], "libraries": []}`,
		"ISBN が不正":     `{"version": 1, "books": [{"isbn": "9784003101019", "title": "P", "addedOn": "2026-10-01"}], "libraries": []}`,
		"1 MiB を超える":   `{"version": 1, "books": [], "libraries": [], "pad": "` + strings.Repeat("x", 1<<20) + `"}`,
		"本文がない":        "",
		"図書館の ID が欠けた": `{"version": 1, "books": [], "libraries": [{"systemId": "", "libKey": "K", "name": "N", "closures": []}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t)
			res := s.postImport(t, body)
			if res.status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %.200s", res.status, res.body)
			}
			if code := res.errorCode(t); code != "invalid" {
				t.Errorf("code = %q", code)
			}
			if got := readDataFile(t, s.cfg.DataDir); got != "" {
				t.Errorf("形式が正しくないのに保存した: %.200s", got)
			}
		})
	}
}
