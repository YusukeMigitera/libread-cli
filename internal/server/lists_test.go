package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GET /api/books・GET /api/libraries は、本の画面で取り込みの欄を出すか決めるのに使う（FR-118）。

const listsData = `{"version": 1,
  "books": [
    {"isbn": "9784003101018", "title": "吾輩は猫である", "author": "夏目漱石", "publisher": "岩波書店", "note": "p.120まで", "addedOn": "2026-10-21"},
    {"isbn": "9784003101100", "title": "坊っちゃん", "addedOn": "2026-10-22"}],
  "libraries": [
    {"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館", "pref": "東京都", "city": "世田谷区",
     "schedule": {"weekly": {"sun": {"open": "09:00", "close": "17:00"}, "mon": {"closed": true}, "tue": {"open": "09:00", "close": "19:00"},
       "wed": {"open": "09:00", "close": "19:00"}, "thu": {"open": "09:00", "close": "19:00"}, "fri": {"open": "09:00", "close": "19:00"},
       "sat": {"open": "09:00", "close": "17:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-21"},
     "closures": [{"start": "2026-12-01", "end": "2026-12-02"}, {"start": "2026-11-10", "end": "2026-11-14", "reason": "特別整理期間"}]},
    {"systemId": "Tokyo_Setagaya", "libKey": "経堂図書館", "name": "世田谷区立経堂図書館",
     "schedule": {"weekly": {"wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "hours", "open": "10:00", "close": "16:00"}, "updatedOn": "2026-10-01"},
     "closures": []},
    {"systemId": "Tokyo_Setagaya", "libKey": "奥沢図書館", "name": "世田谷区立奥沢図書館", "closures": []}]}`

func (s *testServer) getJSON(t *testing.T, target string, out any) []byte {
	t.Helper()
	rec := s.do(t, http.MethodGet, target, "", withCookie(s.sessionCookie(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body = %s", target, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatal(err)
	}
	return rec.Body.Bytes()
}

func TestListBooks(t *testing.T) {
	s := newTestServer(t)
	var empty struct{ Books []bookJSON }
	if body := s.getJSON(t, "/api/books", &empty); string(body) != "{\"books\":[]}\n" {
		t.Errorf("データなし: %s", body)
	}

	s.writeData(t, listsData)
	var got struct{ Books []bookJSON }
	body := s.getJSON(t, "/api/books", &got)
	assertMatchesExample(t, "books", body)
	want := []bookJSON{
		{ISBN: "9784003101018", Title: "吾輩は猫である", Author: "夏目漱石", Publisher: "岩波書店", Note: "p.120まで", AddedOn: "2026-10-21"},
		{ISBN: "9784003101100", Title: "坊っちゃん", AddedOn: "2026-10-22"},
	}
	if len(got.Books) != 2 || got.Books[0] != want[0] || got.Books[1] != want[1] {
		t.Errorf("books = %+v, want %+v", got.Books, want)
	}
}

func TestListLibraries(t *testing.T) {
	s := newTestServer(t)
	var empty map[string]any
	if body := s.getJSON(t, "/api/libraries", &empty); string(body) != "{\"libraries\":[]}\n" {
		t.Errorf("データなし: %s", body)
	}

	s.writeData(t, listsData)
	var got map[string]any
	body := s.getJSON(t, "/api/libraries", &got)
	// 例と形を比べるのは、例と同じくすべての曜日と祝日の扱いを曜日どおりで入力した館。
	// ほかの館は、例でオブジェクトの位置が null（休館・未入力）になったり、祝日の時刻が付いたりする。
	first, err := json.Marshal(map[string]any{"libraries": got["libraries"].([]any)[:1]})
	if err != nil {
		t.Fatal(err)
	}
	assertMatchesExample(t, "libraries", first)

	var typed librariesResponse
	if err := json.Unmarshal(body, &typed); err != nil {
		t.Fatal(err)
	}
	libs := typed.Libraries
	if len(libs) != 3 || libs[0].ID != "Tokyo_Setagaya:中央図書館" || libs[0].Pref != "東京都" || libs[0].City != "世田谷区" {
		t.Fatalf("libraries = %+v", libs)
	}
	w := libs[0].Schedule.Weekly
	if w.Mon != nil || w.Sun == nil || *w.Sun != (hoursJSON{"09:00", "17:00"}) || libs[0].Schedule.Holiday.Mode != "same" {
		t.Errorf("中央図書館の schedule = %+v", libs[0].Schedule)
	}
	if c := libs[0].Closures; len(c) != 2 || c[0] != (closureJSON{"2026-11-10", "2026-11-14", "特別整理期間"}) || c[1].Start != "2026-12-01" {
		t.Errorf("closures は開始日の順: %+v", c)
	}
	if h := libs[1].Schedule.Holiday; h != (holidayJSON{"hours", "10:00", "16:00"}) {
		t.Errorf("経堂図書館の holiday = %+v", h)
	}
	if libs[1].Schedule.Weekly.Sun != nil {
		t.Errorf("入力していない曜日は休館（null）: %+v", libs[1].Schedule.Weekly)
	}
	// 未入力の schedule は null で、キーは残す。
	third := got["libraries"].([]any)[2].(map[string]any)
	if v, ok := third["schedule"]; !ok || v != nil {
		t.Errorf("奥沢図書館の schedule = %v（キーあり = %v）, want null", v, ok)
	}
}
