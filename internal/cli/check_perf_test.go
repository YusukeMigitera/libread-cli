package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// perfISBNs は SC-002 の想定（読みかけの本5冊）に使う ISBN。
var perfISBNs = []string{"9784003101018", "9784003101100", "9784003101025", "9784003101032", "9784003101049"}

// writePerfData は10システムに1館ずつ（計10館）と本5冊を data.json に書き、図書館システムIDを返す。
// どの館も水曜 09:00-19:00 に開館するので、check の時刻（水曜 14:05）にはすべて開館中になる。
func writePerfData(t *testing.T, env Env) []string {
	t.Helper()
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
	raw := `{"version": 1, "books": [` + strings.Join(books, ",") + `], "libraries": [` + strings.Join(libs, ",") + `]}`
	if err := os.WriteFile(filepath.Join(env.DataDir, "data.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return systems
}

// runningResponse は、すべての本・システムが取得中（Running）のカーリルの応答。
func runningResponse(systems []string) string {
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

// SC-002: 利用図書館10館（10システム）・読みかけの本5冊を一度に検索しても、10秒以内に結果を表示する。
// カーリルが取得を終えなくても（Running のまま）、上限で打ち切って「不明（取得タイムアウト）」とする。
func TestCheckPerformanceSC002(t *testing.T) {
	svc := &fakeServices{}
	env, stdout, stderr := newCheckEnv(t, "", svc)
	systems := writePerfData(t, env)
	svc.calil = []string{runningResponse(systems)}

	if code := Run(context.Background(), []string{"check"}, env); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if got := env.Now().Sub(checkStart); got > 10*time.Second {
		t.Errorf("check に %v かかった（SC-002 の上限 10 秒）", got)
	}

	first := svc.recorded()[0].url.Query()
	if first.Get("isbn") != strings.Join(perfISBNs, ",") || first.Get("systemid") != strings.Join(systems, ",") {
		t.Errorf("初回の要求で本5冊 × 10システムをまとめて問い合わせていない: %v", first)
	}
	if svc.calilRequests < 2 {
		t.Errorf("カーリルへの要求 = %d, want ポーリングを含めて2回以上", svc.calilRequests)
	}

	out := stdout.String()
	if !strings.Contains(out, "今日読める図書館はありません") {
		t.Errorf("取得タイムアウトの館が「読める」に含まれた:\n%s", out)
	}
	if n := strings.Count(out, "不明（取得タイムアウト）"); n != len(perfISBNs)*len(systems) {
		t.Errorf("「不明（取得タイムアウト）」= %d 件, want %d 件（10館 × 5冊）:\n%s", n, len(perfISBNs)*len(systems), out)
	}
	for i := range systems {
		if want := fmt.Sprintf("- 図書館%02d  開館中 〜19:00", i+1); !strings.Contains(out, want) {
			t.Errorf("出力に %q がない:\n%s", want, out)
		}
	}
}
