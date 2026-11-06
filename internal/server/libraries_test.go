package server

import (
	"encoding/json"
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

// 利用図書館（contracts/http-api.md「利用図書館」）のテスト。カーリルは記録済みの応答（internal/calil/testdata）で差し替える。

// fakeCalilLibrary はカーリルの図書館 API（/library）の代わりに応答する。受けた要求のクエリーを記録する。
type fakeCalilLibrary struct {
	mu       sync.Mutex
	requests []url.Values
	status   int // 0 または 200 以外なら、その状態コードを返す
}

func (f *fakeCalilLibrary) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.URL.Query())
	status := f.status
	f.mu.Unlock()
	if r.Host != "api.calil.jp" || r.URL.Path != "/library" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	// 図書館システムでの問い合わせ（登録時の確認）と、都道府県での問い合わせ（検索）を見分ける。
	name := "library_setagaya.json"
	if r.URL.Query().Has("systemid") {
		name = "library_system_setagaya.json"
		if r.URL.Query().Get("systemid") != "Tokyo_Setagaya" {
			w.Write([]byte(`[]`))
			return
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "calil", "testdata", name))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(raw)
}

func (f *fakeCalilLibrary) recorded() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.requests...)
}

// libServer は利用図書館の API のテスト用のサーバー。カーリルへの要求は calil に届く。
type libServer struct {
	*testServer
	calil  *fakeCalilLibrary
	cookie *http.Cookie
}

func newLibServer(t *testing.T, mutate ...func(*Config)) *libServer {
	t.Helper()
	calil := &fakeCalilLibrary{}
	srv := httptest.NewServer(calil)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	all := append([]func(*Config){func(c *Config) {
		c.HTTP = &http.Client{Transport: routeTransport{target: target}}
	}}, mutate...)
	ts := newTestServer(t, all...)
	return &libServer{testServer: ts, calil: calil, cookie: ts.sessionCookie(t)}
}

func (s *libServer) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, http.MethodGet, target, "", withCookie(s.cookie))
}

func (s *libServer) send(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, method, target, body, withCookie(s.cookie), withOrigin(testOrigin))
}

// libraries は保存されている利用図書館（GET /api/libraries）。
func (s *libServer) libraries(t *testing.T) []libraryJSON {
	t.Helper()
	var out librariesResponse
	s.getJSON(t, "/api/libraries", &out)
	return out.Libraries
}

// libraryPath は /api/libraries/{id} の URL。id は URL エンコードする。
func libraryPath(id string, rest ...string) string {
	return "/api/libraries/" + url.PathEscape(id) + strings.Join(rest, "")
}

func decodeLibrary(t *testing.T, rec *httptest.ResponseRecorder) libraryJSON {
	t.Helper()
	var l libraryJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatalf("応答が JSON でない: %v\n%s", err, rec.Body)
	}
	return l
}

// assertLibraryMatchesExample は、館1件の応答を {"libraries": [...]} に包んで libraries.json の例と形を比べる。
func assertLibraryMatchesExample(t *testing.T, body []byte) {
	t.Helper()
	assertMatchesExample(t, "libraries", []byte(`{"libraries": [`+string(body)+`]}`))
}

const (
	centralID = "Tokyo_Setagaya:中央図書館"
	kyodoID   = "Tokyo_Setagaya:経堂図書館"
)

// libsData は、すべての曜日と祝日の扱いを入力した中央図書館（臨時休館期間は開始日の順に並んでいない）と、開館情報未入力の経堂図書館。
const libsData = `{"version": 1,
  "books": [],
  "libraries": [
    {"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館", "pref": "東京都", "city": "世田谷区",
     "schedule": {"weekly": {"sun": {"open": "09:00", "close": "17:00"}, "mon": {"closed": true}, "tue": {"open": "09:00", "close": "19:00"},
       "wed": {"open": "09:00", "close": "19:00"}, "thu": {"open": "09:00", "close": "19:00"}, "fri": {"open": "09:00", "close": "19:00"},
       "sat": {"open": "09:00", "close": "17:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-21"},
     "closures": [{"start": "2026-12-01", "end": "2026-12-02"}, {"start": "2026-11-10", "end": "2026-11-14", "reason": "特別整理期間"}]},
    {"systemId": "Tokyo_Setagaya", "libKey": "経堂図書館", "name": "世田谷区立経堂図書館", "pref": "東京都", "city": "世田谷区", "closures": []}]}`

// fullWeek は7曜日すべてを持つ開館スケジュール（月曜は休館）。
const fullWeek = `{"sun": {"open": "09:00", "close": "17:00"}, "mon": null, "tue": {"open": "09:00", "close": "19:00"},
  "wed": {"open": "10:00", "close": "20:00"}, "thu": {"open": "09:00", "close": "19:00"}, "fri": {"open": "09:00", "close": "19:00"},
  "sat": {"open": "09:00", "close": "17:00"}}`

// 図書館の検索（GET /api/library-search）

type librarySearchBody struct {
	Libraries []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Pref       string `json:"pref"`
		City       string `json:"city"`
		Address    string `json:"address"`
		Registered bool   `json:"registered"`
	} `json:"libraries"`
	Credit struct {
		Label string `json:"label"`
		URL   string `json:"url"`
	} `json:"credit"`
}

func TestLibrarySearch(t *testing.T) {
	s := newLibServer(t)
	s.writeData(t, libsData)

	rec := s.get(t, "/api/library-search?pref="+url.QueryEscape("東京都")+"&city="+url.QueryEscape("世田谷区"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertMatchesExample(t, "library-search", rec.Body.Bytes())
	var got librarySearchBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Libraries) != 35 {
		t.Fatalf("libraries = %d件, want 35（記録済みの応答のすべて）", len(got.Libraries))
	}
	first := got.Libraries[0]
	if first.ID != "Tokyo_Setagaya:上北沢図書館" || first.Name != "世田谷区立上北沢図書館" || first.Pref != "東京都" ||
		first.City != "世田谷区" || first.Address != "東京都世田谷区上北沢3-8-9 上北沢区民センター地階" || first.Registered {
		t.Errorf("1件目 = %+v", first)
	}
	registered := map[string]bool{}
	for _, l := range got.Libraries {
		registered[l.ID] = l.Registered
	}
	if !registered[centralID] || !registered[kyodoID] || registered["Tokyo_Setagaya:奥沢図書館"] {
		t.Errorf("registered = %v", registered)
	}
	if got.Credit.Label != "図書館情報: カーリル" || got.Credit.URL != "https://calil.jp/" {
		t.Errorf("credit = %+v", got.Credit)
	}

	reqs := s.calil.recorded()
	if len(reqs) != 1 {
		t.Fatalf("カーリルへの要求 = %d, want 1", len(reqs))
	}
	if q := reqs[0]; q.Get("pref") != "東京都" || q.Get("city") != "世田谷区" || q.Get("appkey") != "test-appkey" {
		t.Errorf("カーリルへの要求のクエリー = %v", q)
	}
}

func TestLibrarySearchFiltersByName(t *testing.T) {
	s := newLibServer(t)
	rec := s.get(t, "/api/library-search?pref="+url.QueryEscape("東京都")+"&name="+url.QueryEscape("図書室"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got librarySearchBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range got.Libraries {
		names = append(names, l.Name)
	}
	want := []string{"世田谷区立喜多見図書室", "世田谷区立希望丘図書室", "世田谷区立松沢図書室", "世田谷区立池尻図書室", "世田谷区立野毛図書室"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", names, want)
	}
	if q := s.calil.recorded()[0]; q.Has("city") || q.Has("name") {
		t.Errorf("市区町村を指定しないときは city を送らず、館名はカーリルに送らない: %v", q)
	}

	// 一致する館がなければ空の配列。
	rec = s.get(t, "/api/library-search?pref="+url.QueryEscape("東京都")+"&name="+url.QueryEscape("存在しない館"))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), `{"libraries":[],`) {
		t.Errorf("一致なし: %d %s", rec.Code, rec.Body)
	}
}

func TestLibrarySearchErrors(t *testing.T) {
	for _, target := range []string{"/api/library-search", "/api/library-search?pref=", "/api/library-search?pref=%20&city=" + url.QueryEscape("世田谷区")} {
		s := newLibServer(t)
		rec := s.get(t, target)
		if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec) != "invalid" {
			t.Errorf("%s: %d %s, want 400 invalid", target, rec.Code, rec.Body)
		}
		if n := len(s.calil.recorded()); n != 0 {
			t.Errorf("%s: カーリルへの要求 = %d, want 0", target, n)
		}
	}

	s := newLibServer(t, func(c *Config) { c.CalilAppKey = "" })
	rec := s.get(t, "/api/library-search?pref="+url.QueryEscape("東京都"))
	if rec.Code != http.StatusServiceUnavailable || errorCodeOf(t, rec) != "notConfigured" {
		t.Errorf("キーなし: %d %s, want 503 notConfigured", rec.Code, rec.Body)
	}

	s = newLibServer(t)
	s.calil.status = http.StatusInternalServerError
	rec = s.get(t, "/api/library-search?pref="+url.QueryEscape("東京都"))
	if rec.Code != http.StatusBadGateway || errorCodeOf(t, rec) != "upstream" {
		t.Errorf("カーリルの失敗: %d %s, want 502 upstream", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "test-appkey") || strings.Contains(s.log.String(), "test-appkey") {
		t.Errorf("アプリケーションキーが応答かログに含まれている:\n%s\n%s", rec.Body, s.log)
	}
}

// 図書館の登録（POST /api/libraries）

func TestAddLibrary(t *testing.T) {
	s := newLibServer(t)
	s.writeData(t, libsData)

	rec := s.send(t, http.MethodPost, "/api/libraries", `{"id": "Tokyo_Setagaya:奥沢図書館"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got := decodeLibrary(t, rec)
	if got.ID != "Tokyo_Setagaya:奥沢図書館" || got.Name != "世田谷区立奥沢図書館" || got.Pref != "東京都" || got.City != "世田谷区" ||
		got.Schedule != nil || got.Closures == nil || len(got.Closures) != 0 {
		t.Errorf("登録した館 = %+v", got)
	}
	if !strings.Contains(rec.Body.String(), `"schedule":null`) || !strings.Contains(rec.Body.String(), `"closures":[]`) {
		t.Errorf("未入力の schedule は null、closures は空の配列: %s", rec.Body)
	}
	reqs := s.calil.recorded()
	if len(reqs) != 1 || reqs[0].Get("systemid") != "Tokyo_Setagaya" {
		t.Errorf("カーリルへの要求 = %v, want systemid=Tokyo_Setagaya の1件", reqs)
	}

	// 登録順の最後に加わる。
	libs := s.libraries(t)
	if len(libs) != 3 || libs[0].ID != centralID || libs[1].ID != kyodoID || libs[2].ID != "Tokyo_Setagaya:奥沢図書館" {
		t.Errorf("libraries = %+v", libs)
	}
}

func TestAddLibraryErrors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		mutate   func(*Config)
		calil    int
		status   int
		code     string
		requests int
	}{
		{name: "形式が違う", body: `{"id": "Tokyo_Setagaya"}`, status: 400, code: "invalid"},
		{name: "JSON でない", body: `{`, status: 400, code: "invalid"},
		{name: "id なし", body: `{}`, status: 400, code: "invalid"},
		{name: "カーリルにない館", body: `{"id": "Tokyo_Setagaya:存在しない館"}`, status: 404, code: "notFound", requests: 1},
		{name: "カーリルにない図書館システム", body: `{"id": "Nowhere:中央"}`, status: 404, code: "notFound", requests: 1},
		{name: "登録済み", body: `{"id": "Tokyo_Setagaya:中央図書館"}`, status: 409, code: "conflict"},
		{name: "キーなし", body: `{"id": "Tokyo_Setagaya:奥沢図書館"}`, mutate: func(c *Config) { c.CalilAppKey = "" }, status: 503, code: "notConfigured"},
		{name: "カーリルの失敗", body: `{"id": "Tokyo_Setagaya:奥沢図書館"}`, calil: 500, status: 502, code: "upstream", requests: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mutate []func(*Config)
			if tt.mutate != nil {
				mutate = append(mutate, tt.mutate)
			}
			s := newLibServer(t, mutate...)
			s.calil.status = tt.calil
			s.writeData(t, libsData)
			before := readDataFile(t, s.cfg.DataDir)

			rec := s.send(t, http.MethodPost, "/api/libraries", tt.body)
			if rec.Code != tt.status || errorCodeOf(t, rec) != tt.code {
				t.Errorf("%d %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
			if n := len(s.calil.recorded()); n != tt.requests {
				t.Errorf("カーリルへの要求 = %d, want %d", n, tt.requests)
			}
			if after := readDataFile(t, s.cfg.DataDir); after != before {
				t.Errorf("保存したデータが変わった:\n%s", after)
			}
		})
	}
}

// 図書館の削除（DELETE /api/libraries/{id}）

func TestRemoveLibrary(t *testing.T) {
	s := newLibServer(t)
	s.writeData(t, libsData)

	rec := s.send(t, http.MethodDelete, libraryPath(centralID), "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %s, want 204", rec.Code, rec.Body)
	}
	if libs := s.libraries(t); len(libs) != 1 || libs[0].ID != kyodoID {
		t.Errorf("libraries = %+v", libs)
	}

	for _, id := range []string{centralID, "Tokyo_Setagaya:奥沢図書館", "Tokyo_Setagaya"} {
		rec := s.send(t, http.MethodDelete, libraryPath(id), "")
		if rec.Code != http.StatusNotFound || errorCodeOf(t, rec) != "notFound" {
			t.Errorf("%s: %d %s, want 404 notFound", id, rec.Code, rec.Body)
		}
	}
	if n := len(s.calil.recorded()); n != 0 {
		t.Errorf("削除でカーリルに問い合わせた: %d", n)
	}
}

// 開館スケジュール（PUT /api/libraries/{id}/schedule）

func TestPutSchedule(t *testing.T) {
	s := newLibServer(t)
	s.writeData(t, libsData)

	rec := s.send(t, http.MethodPut, libraryPath(kyodoID, "/schedule"), `{"weekly": `+fullWeek+`, "holiday": {"mode": "same"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertLibraryMatchesExample(t, rec.Body.Bytes())
	got := decodeLibrary(t, rec)
	if got.ID != kyodoID || got.Schedule == nil {
		t.Fatalf("更新後の館 = %+v", got)
	}
	w := got.Schedule.Weekly
	if w.Mon != nil || w.Wed == nil || *w.Wed != (hoursJSON{"10:00", "20:00"}) || *w.Sun != (hoursJSON{"09:00", "17:00"}) {
		t.Errorf("weekly = %+v", w)
	}
	if got.Schedule.Holiday != (holidayJSON{Mode: "same"}) {
		t.Errorf("holiday = %+v", got.Schedule.Holiday)
	}
	if got.Schedule.UpdatedOn != "2026-11-05" {
		t.Errorf("updatedOn = %q, want 今日（2026-11-05）", got.Schedule.UpdatedOn)
	}
	// 保存されている。
	libs := s.libraries(t)
	if libs[1].Schedule == nil || *libs[1].Schedule != *got.Schedule {
		t.Errorf("保存した schedule = %+v", libs[1].Schedule)
	}

	// 祝日の扱いを時間の指定に。丸ごと置き換える（曜日も変わる）。
	rec = s.send(t, http.MethodPut, libraryPath(kyodoID, "/schedule"),
		`{"weekly": {"sun": null, "mon": null, "tue": null, "wed": {"open": "09:00", "close": "12:00"}, "thu": null, "fri": null, "sat": null},
		  "holiday": {"mode": "hours", "open": "10:00", "close": "16:00"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got = decodeLibrary(t, rec)
	if h := got.Schedule.Holiday; h != (holidayJSON{"hours", "10:00", "16:00"}) {
		t.Errorf("holiday = %+v", h)
	}
	if w := got.Schedule.Weekly; w.Sun != nil || w.Wed == nil || *w.Wed != (hoursJSON{"09:00", "12:00"}) {
		t.Errorf("weekly = %+v", w)
	}

	rec = s.send(t, http.MethodPut, libraryPath(kyodoID, "/schedule"), `{"weekly": `+fullWeek+`, "holiday": {"mode": "closed"}}`)
	if rec.Code != http.StatusOK || decodeLibrary(t, rec).Schedule.Holiday != (holidayJSON{Mode: "closed"}) {
		t.Errorf("祝日は休館: %d %s", rec.Code, rec.Body)
	}
	// 臨時休館期間はそのまま。
	if got := s.libraries(t)[0]; len(got.Closures) != 2 {
		t.Errorf("ほかの館の closures が変わった: %+v", got.Closures)
	}
}

func TestPutScheduleErrors(t *testing.T) {
	withoutMon := strings.Replace(fullWeek, `"mon": null, `, "", 1)
	tests := []struct {
		name, id, body string
		status         int
		code           string
	}{
		{"曜日が欠けている", kyodoID, `{"weekly": ` + withoutMon + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"知らない曜日", kyodoID, `{"weekly": ` + strings.Replace(fullWeek, `"mon"`, `"xyz"`, 1) + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"weekly なし", kyodoID, `{"holiday": {"mode": "same"}}`, 400, "invalid"},
		{"時刻の形式", kyodoID, `{"weekly": ` + strings.Replace(fullWeek, `"10:00"`, `"9:00"`, 1) + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"時刻の範囲", kyodoID, `{"weekly": ` + strings.Replace(fullWeek, `"20:00"`, `"24:30"`, 1) + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"開館が閉館より後", kyodoID, `{"weekly": ` + strings.Replace(fullWeek, `"20:00"`, `"08:00"`, 1) + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"開館と閉館が同じ", kyodoID, `{"weekly": ` + strings.Replace(fullWeek, `"20:00"`, `"10:00"`, 1) + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"閉館時刻なし", kyodoID, `{"weekly": ` + strings.Replace(fullWeek, `, "close": "20:00"`, "", 1) + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"曜日の値が文字列", kyodoID, `{"weekly": ` + strings.Replace(fullWeek, `"mon": null`, `"mon": "closed"`, 1) + `, "holiday": {"mode": "same"}}`, 400, "invalid"},
		{"祝日の扱いなし", kyodoID, `{"weekly": ` + fullWeek + `}`, 400, "invalid"},
		{"知らない祝日の扱い", kyodoID, `{"weekly": ` + fullWeek + `, "holiday": {"mode": "open"}}`, 400, "invalid"},
		{"祝日の時刻なし", kyodoID, `{"weekly": ` + fullWeek + `, "holiday": {"mode": "hours", "open": "10:00"}}`, 400, "invalid"},
		{"祝日の時刻が逆", kyodoID, `{"weekly": ` + fullWeek + `, "holiday": {"mode": "hours", "open": "16:00", "close": "10:00"}}`, 400, "invalid"},
		{"JSON でない", kyodoID, `{`, 400, "invalid"},
		{"登録していない館", "Tokyo_Setagaya:奥沢図書館", `{"weekly": ` + fullWeek + `, "holiday": {"mode": "same"}}`, 404, "notFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newLibServer(t)
			s.writeData(t, libsData)
			before := readDataFile(t, s.cfg.DataDir)
			rec := s.send(t, http.MethodPut, libraryPath(tt.id, "/schedule"), tt.body)
			if rec.Code != tt.status || errorCodeOf(t, rec) != tt.code {
				t.Errorf("%d %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
			if after := readDataFile(t, s.cfg.DataDir); after != before {
				t.Errorf("保存したデータが変わった:\n%s", after)
			}
		})
	}
}

// 臨時休館期間（POST /api/libraries/{id}/closures・DELETE /api/libraries/{id}/closures/{index}）

func TestAddClosure(t *testing.T) {
	s := newLibServer(t)
	s.writeData(t, libsData)

	// 開始日の順に並べ直して返す。重なる期間も登録できる。
	rec := s.send(t, http.MethodPost, libraryPath(centralID, "/closures"), `{"start": "2026-11-12", "end": "2026-11-20", "reason": "蔵書点検"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertLibraryMatchesExample(t, rec.Body.Bytes())
	got := decodeLibrary(t, rec)
	want := []closureJSON{
		{"2026-11-10", "2026-11-14", "特別整理期間"},
		{"2026-11-12", "2026-11-20", "蔵書点検"},
		{"2026-12-01", "2026-12-02", ""},
	}
	if len(got.Closures) != 3 || got.Closures[0] != want[0] || got.Closures[1] != want[1] || got.Closures[2] != want[2] {
		t.Errorf("closures = %+v, want %+v", got.Closures, want)
	}
	if c := s.libraries(t)[0].Closures; len(c) != 3 || c[1] != want[1] {
		t.Errorf("保存した closures = %+v", c)
	}

	// 理由は省略できる。1日だけの期間も登録できる。
	rec = s.send(t, http.MethodPost, libraryPath(kyodoID, "/closures"), `{"start": "2026-11-05", "end": "2026-11-05"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if c := decodeLibrary(t, rec).Closures; len(c) != 1 || c[0] != (closureJSON{"2026-11-05", "2026-11-05", ""}) {
		t.Errorf("closures = %+v", c)
	}
}

func TestAddClosureErrors(t *testing.T) {
	tests := []struct {
		name, id, body string
		status         int
		code           string
	}{
		{"開始日が終了日より後", centralID, `{"start": "2026-11-15", "end": "2026-11-14"}`, 400, "invalid"},
		{"日付の形式", centralID, `{"start": "2026/11/10", "end": "2026-11-14"}`, 400, "invalid"},
		{"存在しない日付", centralID, `{"start": "2026-02-30", "end": "2026-03-01"}`, 400, "invalid"},
		{"終了日なし", centralID, `{"start": "2026-11-10"}`, 400, "invalid"},
		{"JSON でない", centralID, `{`, 400, "invalid"},
		{"同じ期間と理由", centralID, `{"start": "2026-11-10", "end": "2026-11-14", "reason": "特別整理期間"}`, 409, "conflict"},
		{"同じ期間（理由なし）", centralID, `{"start": "2026-12-01", "end": "2026-12-02"}`, 409, "conflict"},
		{"登録していない館", "Tokyo_Setagaya:奥沢図書館", `{"start": "2026-11-10", "end": "2026-11-14"}`, 404, "notFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newLibServer(t)
			s.writeData(t, libsData)
			before := readDataFile(t, s.cfg.DataDir)
			rec := s.send(t, http.MethodPost, libraryPath(tt.id, "/closures"), tt.body)
			if rec.Code != tt.status || errorCodeOf(t, rec) != tt.code {
				t.Errorf("%d %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
			if after := readDataFile(t, s.cfg.DataDir); after != before {
				t.Errorf("保存したデータが変わった:\n%s", after)
			}
		})
	}

	// 同じ期間でも理由が違えば別の期間として登録できる。
	s := newLibServer(t)
	s.writeData(t, libsData)
	if rec := s.send(t, http.MethodPost, libraryPath(centralID, "/closures"), `{"start": "2026-11-10", "end": "2026-11-14", "reason": "別の理由"}`); rec.Code != http.StatusCreated {
		t.Errorf("理由が違う: %d %s, want 201", rec.Code, rec.Body)
	}
}

func TestRemoveClosure(t *testing.T) {
	s := newLibServer(t)
	s.writeData(t, libsData)

	// 位置は GET /api/libraries の closures（開始日の順）の位置。保存されている順ではない。
	rec := s.send(t, http.MethodDelete, libraryPath(centralID, "/closures/0"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertLibraryMatchesExample(t, rec.Body.Bytes())
	if c := decodeLibrary(t, rec).Closures; len(c) != 1 || c[0] != (closureJSON{"2026-12-01", "2026-12-02", ""}) {
		t.Errorf("closures = %+v, want 2026-12-01〜2026-12-02 だけ", c)
	}
	if c := s.libraries(t)[0].Closures; len(c) != 1 || c[0].Start != "2026-12-01" {
		t.Errorf("保存した closures = %+v", c)
	}

	for _, target := range []string{
		libraryPath(centralID, "/closures/1"),
		libraryPath(centralID, "/closures/-1"),
		libraryPath(centralID, "/closures/x"),
		libraryPath(centralID, "/closures/01x"),
		libraryPath(kyodoID, "/closures/0"),
		libraryPath("Tokyo_Setagaya:奥沢図書館", "/closures/0"),
	} {
		rec := s.send(t, http.MethodDelete, target, "")
		if rec.Code != http.StatusNotFound || errorCodeOf(t, rec) != "notFound" {
			t.Errorf("%s: %d %s, want 404 notFound", target, rec.Code, rec.Body)
		}
	}
	if c := s.libraries(t)[0].Closures; len(c) != 1 {
		t.Errorf("失敗した削除で closures が変わった: %+v", c)
	}
}

// US3 シナリオ 3・4: 開館スケジュールと今日を含む臨時休館期間を入力すると、今日の判定に反映される。削除すると曜日の開館時間に戻る。
func TestScheduleAndClosureChangeCheck(t *testing.T) {
	svc := &fakeServices{calil: []string{contractCheckFinal}, holidays: `{}`}
	s := newCheckServer(t, svc, checkStart) // 2026-10-21（水）14:05
	s.writeData(t, `{"version": 1,
	  "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "addedOn": "2026-10-21"}],
	  "libraries": [{"systemId": "Tokyo_Setagaya", "libKey": "中央図書館", "name": "世田谷区立中央図書館", "closures": []}]}`)
	send := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := s.do(t, method, target, body, withCookie(s.cookie), withOrigin(testOrigin))
		if rec.Code/100 != 2 {
			t.Fatalf("%s %s: %d %s", method, target, rec.Code, rec.Body)
		}
		return rec
	}
	opening := func() (string, string) {
		t.Helper()
		var got checkJSONBody
		raw, _ := json.Marshal(s.checkJSON(t, "/api/check"))
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		libs := append(got.Readable, got.NotReadable...)
		if len(libs) != 1 {
			t.Fatalf("libraries = %+v", libs)
		}
		return libs[0].Opening.Status, libs[0].Opening.Label
	}

	if st, _ := opening(); st != "noSchedule" {
		t.Errorf("入力前 = %s, want noSchedule", st)
	}
	send(http.MethodPut, libraryPath(centralID, "/schedule"), `{"weekly": `+fullWeek+`, "holiday": {"mode": "same"}}`)
	if st, label := opening(); st != "open" || label != "開館中 〜20:00" {
		t.Errorf("スケジュールの入力後 = %s %s, want open 開館中 〜20:00", st, label)
	}
	send(http.MethodPost, libraryPath(centralID, "/closures"), `{"start": "2026-10-20", "end": "2026-10-22", "reason": "蔵書点検"}`)
	if st, _ := opening(); st != "closedPeriod" {
		t.Errorf("今日を含む臨時休館の入力後 = %s, want closedPeriod", st)
	}
	send(http.MethodDelete, libraryPath(centralID, "/closures/0"), "")
	if st, label := opening(); st != "open" || label != "開館中 〜20:00" {
		t.Errorf("臨時休館の削除後 = %s %s, want open 開館中 〜20:00", st, label)
	}
	if n := svc.sessions(); n != 1 {
		t.Errorf("カーリルのセッション = %d, want 1（2回目からは30分以内のキャッシュ）", n)
	}
}

// research W9: 図書館の検索・登録と所蔵の検索が同時に来ても、カーリルへの要求は同時に1つまで。
func TestLibraryEndpointsDoNotOverlapAtCalil(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "calil", "testdata", "library_setagaya.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &fakeServices{calil: []string{exampleRunning, exampleFinal}, calilLibrary: string(raw), holidays: `{}`, delay: 10 * time.Millisecond}
	s := newCheckServer(t, svc, checkStart)
	s.writeDataFile(t, "check_example.json")

	var wg sync.WaitGroup
	run := func(method, target, body string, want int) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := s.do(t, method, target, body, withCookie(s.cookie), withOrigin(testOrigin))
			if rec.Code != want {
				t.Errorf("%s %s: status = %d, body = %s", method, target, rec.Code, rec.Body)
			}
		}()
	}
	for range 2 {
		run(http.MethodGet, "/api/check", "", http.StatusOK)
		run(http.MethodGet, "/api/library-search?pref="+url.QueryEscape("東京都"), "", http.StatusOK)
	}
	run(http.MethodPost, "/api/libraries", `{"id": "Tokyo_Setagaya:代田図書館"}`, http.StatusCreated)
	wg.Wait()
	if m := svc.calilMaxInFlight.Load(); m != 1 {
		t.Errorf("カーリルへの同時の要求 = %d, want 1", m)
	}
}
