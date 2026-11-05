package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"libread/internal/domain"
)

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustHours(t *testing.T, open, close string) domain.Hours {
	t.Helper()
	o, err := domain.ParseTimeOfDay(open)
	if err != nil {
		t.Fatal(err)
	}
	c, err := domain.ParseTimeOfDay(close)
	if err != nil {
		t.Fatal(err)
	}
	h, err := domain.NewHours(o, c)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func sampleData(t *testing.T) Data {
	t.Helper()
	isbn, err := domain.ParseISBN("9784003101018")
	if err != nil {
		t.Fatal(err)
	}
	book, err := domain.NewBook(isbn, "吾輩は猫である", "夏目漱石", "岩波書店")
	if err != nil {
		t.Fatal(err)
	}
	closure, err := domain.NewClosurePeriod(mustDate(t, "2026-11-10"), mustDate(t, "2026-11-14"), "特別整理期間")
	if err != nil {
		t.Fatal(err)
	}
	var weekly [7]domain.DayRule
	weekly[time.Sunday] = domain.OpenDay(mustHours(t, "09:00", "17:00"))
	weekly[time.Tuesday] = domain.OpenDay(mustHours(t, "09:00", "19:00"))
	return Data{
		Books: []domain.ReadingBook{
			{Book: book, Note: "p.120まで", AddedOn: mustDate(t, "2026-10-21")},
		},
		Libraries: []domain.RegisteredLibrary{
			{
				ID:   domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "中央"},
				Name: "世田谷区立中央図書館",
				Pref: "東京都",
				City: "世田谷区",
				Schedule: &domain.Schedule{
					Weekly:    weekly,
					Holiday:   domain.HolidayCustomHours(mustHours(t, "10:00", "16:00")),
					UpdatedOn: mustDate(t, "2026-10-21"),
				},
				Closures: []domain.ClosurePeriod{closure},
			},
			{
				ID:       domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "経堂"},
				Name:     "世田谷区立経堂図書館",
				Pref:     "東京都",
				City:     "世田谷区",
				Closures: []domain.ClosurePeriod{},
			},
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := sampleData(t)
	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch\n got: %+v\nwant: %+v", got, want)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version": 1`) {
		t.Errorf("data.json に version 1 がない:\n%s", raw)
	}
}

func TestLoadDocumentedFormat(t *testing.T) {
	// data-model.md「永続化形式」の例。weekly に書かれていない曜日は休館。
	dir := t.TempDir()
	doc := `{
  "version": 1,
  "books": [
    {"isbn": "9784003101018", "title": "吾輩は猫である", "author": "夏目漱石", "publisher": "岩波書店",
     "note": "p.120まで", "addedOn": "2026-10-21"}
  ],
  "libraries": [
    {"systemId": "Tokyo_Setagaya", "libKey": "中央", "name": "世田谷区立中央図書館",
     "pref": "東京都", "city": "世田谷区",
     "schedule": {
       "weekly": {"sun": {"open": "09:00", "close": "17:00"}, "mon": {"closed": true}},
       "holiday": {"mode": "same"},
       "updatedOn": "2026-10-21"
     },
     "closures": [{"start": "2026-11-10", "end": "2026-11-14", "reason": "特別整理期間"}]}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Libraries) != 1 || got.Libraries[0].Schedule == nil {
		t.Fatalf("libraries = %+v", got.Libraries)
	}
	s := got.Libraries[0].Schedule
	if h, open := s.Weekly[time.Sunday].Hours(); !open || h != mustHours(t, "09:00", "17:00") {
		t.Errorf("sun = %v, %v", h, open)
	}
	for _, wd := range []time.Weekday{time.Monday, time.Tuesday, time.Saturday} {
		if _, open := s.Weekly[wd].Hours(); open {
			t.Errorf("%v は休館のはず", wd)
		}
	}
	if s.Holiday.Mode() != domain.HolidaySameAsWeekday {
		t.Errorf("holiday mode = %v", s.Holiday.Mode())
	}
	if got.Books[0].Book.Title != "吾輩は猫である" {
		t.Errorf("book = %+v", got.Books[0])
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Books) != 0 || len(got.Libraries) != 0 {
		t.Errorf("got %+v, want empty", got)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"未知のversion": `{"version": 2, "books": [], "libraries": []}`,
		"versionなし":  `{"books": [], "libraries": []}`,
		"不正なISBN":    `{"version": 1, "books": [{"isbn": "123", "title": "x", "addedOn": "2026-10-21"}], "libraries": []}`,
		"書名なし":       `{"version": 1, "books": [{"isbn": "9784003101018", "title": "", "addedOn": "2026-10-21"}], "libraries": []}`,
		"未知の曜日":      `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "n", "schedule": {"weekly": {"xyz": {"closed": true}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-21"}}]}`,
		"未知の祝日mode":  `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "n", "schedule": {"weekly": {}, "holiday": {"mode": "sometimes"}, "updatedOn": "2026-10-21"}}]}`,
		"開館>閉館":      `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "n", "schedule": {"weekly": {"sun": {"open": "17:00", "close": "09:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-21"}}]}`,
		"休館期間の開始>終了": `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "n", "closures": [{"start": "2026-11-14", "end": "2026-11-10"}]}]}`,
		"壊れたJSON":    `{"version": 1,`,
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir); err == nil {
				t.Error("want error")
			}
		})
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	d := sampleData(t)
	for range 2 { // 既存ファイルの上書きも確かめる
		if err := Save(dir, d); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "data.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir entries = %v, want [data.json]", names)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "libread")
	if err := Save(dir, Data{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestResolveDataDir(t *testing.T) {
	got, err := ResolveDataDir("/tmp/custom")
	if err != nil || got != "/tmp/custom" {
		t.Errorf("override: got %q, %v", got, err)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skip("UserConfigDir を取得できない環境")
	}
	got, err = ResolveDataDir("")
	if err != nil || got != filepath.Join(base, "libread") {
		t.Errorf("default: got %q, %v; want %q", got, err, filepath.Join(base, "libread"))
	}
}

// ParseData は Load と同じ内容を受け付け、同じ Data を返す（research W8: 取り込みで使う）。
func TestParseDataMatchesLoad(t *testing.T) {
	for _, name := range []string{"check_scenario1.json", "check_example.json", "check_edge.json", "check_single.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "cli", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "data.json"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			want, err := Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got, err := ParseData(raw)
			if err != nil {
				t.Fatalf("ParseData: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ParseData と Load の結果が違う\n got: %+v\nwant: %+v", got, want)
			}
		})
	}
}

func TestParseDataRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"壊れたJSON":    `{"version": 1,`,
		"未知のversion": `{"version": 2, "books": [], "libraries": []}`,
		"versionなし":  `{"books": [], "libraries": []}`,
		"不正なISBN":    `{"version": 1, "books": [{"isbn": "9784003101019", "title": "x", "addedOn": "2026-10-21"}], "libraries": []}`,
		"開館>=閉館":     `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "n", "schedule": {"weekly": {"sun": {"open": "09:00", "close": "09:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-21"}}]}`,
		"休館期間の開始>終了": `{"version": 1, "books": [], "libraries": [{"systemId": "S", "libKey": "K", "name": "n", "closures": [{"start": "2026-11-14", "end": "2026-11-10"}]}]}`,
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseData([]byte(doc)); err == nil {
				t.Error("want error")
			}
		})
	}
}
