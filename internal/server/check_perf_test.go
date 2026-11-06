package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 001 の internal/cli/check_perf_test.go を API に移したもの。

// perfISBNs は SC-102 の想定（読みかけの本5冊）に使う ISBN。
var perfISBNs = []string{"9784003101018", "9784003101100", "9784003101025", "9784003101032", "9784003101049"}

// perfData は10システムに1館ずつ（計10館）と本5冊の data.json と、図書館システムIDを返す。
// どの館も水曜 09:00-19:00 に開館するので、検索の時刻（水曜 14:05）にはすべて開館中になる。
func perfData() (string, []string) {
	var books, libs, systems []string
	for i, isbn := range perfISBNs {
		books = append(books, fmt.Sprintf(`{"isbn": %q, "title": "本%d", "addedOn": "2026-10-01"}`, isbn, i+1))
	}
	for i := range 10 {
		system := fmt.Sprintf("Test_System%02d", i+1)
		systems = append(systems, system)
		libs = append(libs, fmt.Sprintf(`{"systemId": %q, "libKey": "中央", "name": "図書館%02d",
		  "schedule": {"weekly": {"wed": {"open": "09:00", "close": "19:00"}}, "holiday": {"mode": "same"}, "updatedOn": "2026-10-01"},
		  "closures": []}`, system, i+1))
	}
	return `{"version": 1, "books": [` + strings.Join(books, ",") + `], "libraries": [` + strings.Join(libs, ",") + `]}`, systems
}

// perfRunning は、すべての本・システムが取得中（Running）のカーリルの応答。
func perfRunning(systems []string) string {
	var books []string
	for _, isbn := range perfISBNs {
		var states []string
		for _, s := range systems {
			states = append(states, fmt.Sprintf(`%q: {"status": "Running", "libkey": {}}`, s))
		}
		books = append(books, fmt.Sprintf(`%q: {%s}`, isbn, strings.Join(states, ",")))
	}
	return `{"session": "S-1", "continue": 1, "books": {` + strings.Join(books, ",") + `}}`
}

// SC-102: 利用図書館10館（10システム）・読みかけの本5冊を一度に検索しても、10秒以内に結果を返す。
// カーリルが取得を終えなくても（Running のまま）、上限で打ち切って「不明（取得タイムアウト）」とする。
// 時刻は注入した時計で測る（カーリルのポーリングの待ち時間は時計を進めるだけ）。
func TestCheckPerformanceSC102(t *testing.T) {
	data, systems := perfData()
	svc := &fakeServices{calil: []string{perfRunning(systems)}}
	s := newCheckServer(t, svc, checkStart)
	s.writeData(t, data)

	rec := s.get(t, "/api/check")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if got := s.clock.Now().Sub(checkStart); got > 10*time.Second {
		t.Errorf("/api/check に %v かかった（SC-102 の上限 10 秒）", got)
	}

	reqs := svc.recorded()
	first := reqs[0].url.Query()
	if first.Get("isbn") != strings.Join(perfISBNs, ",") || first.Get("systemid") != strings.Join(systems, ",") {
		t.Errorf("初回の要求で本5冊 × 10システムをまとめて問い合わせていない: %v", first)
	}
	if svc.calilRequests < 2 {
		t.Errorf("カーリルへの要求 = %d, want ポーリングを含めて2回以上", svc.calilRequests)
	}

	var body checkJSONBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Readable) != 0 {
		t.Errorf("取得タイムアウトの館が「読める」に含まれた: %d 館", len(body.Readable))
	}
	if len(body.NotReadable) != len(systems) {
		t.Fatalf("読めない館 = %d, want %d", len(body.NotReadable), len(systems))
	}
	timeouts := 0
	for i, lib := range body.NotReadable {
		if want := fmt.Sprintf("図書館%02d", i+1); lib.Library.Name != want {
			t.Errorf("%d 番目の館 = %q, want %q", i, lib.Library.Name, want)
		}
		if lib.Opening.Status != "open" || lib.Opening.Close != "19:00" {
			t.Errorf("%s の開館状況 = %+v, want 開館中 〜19:00", lib.Library.Name, lib.Opening)
		}
		for _, b := range lib.Books {
			if b.Label == "不明（取得タイムアウト）" && !b.Readable {
				timeouts++
			}
		}
	}
	if want := len(perfISBNs) * len(systems); timeouts != want {
		t.Errorf("「不明（取得タイムアウト）」= %d 件, want %d 件（10館 × 5冊）\n%s", timeouts, want, rec.Body)
	}
}
