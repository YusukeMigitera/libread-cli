package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"libread/internal/domain"
)

// calilFixture は internal/calil/testdata のカーリルの応答。
func calilFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "calil", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// findLibrary は data.json から id の利用図書館を返す。
func findLibrary(t *testing.T, env Env, id string) domain.RegisteredLibrary {
	t.Helper()
	for _, l := range loadData(t, env).Libraries {
		if l.ID.String() == id {
			return l
		}
	}
	t.Fatalf("利用図書館 %s が登録されていない", id)
	return domain.RegisteredLibrary{}
}

func dayText(rule domain.DayRule) string {
	if h, open := rule.Hours(); open {
		return h.String()
	}
	return "closed"
}

// US3 受け入れシナリオ 1・3: 都道府県を選んで図書館を探して登録し、開館スケジュールと臨時休館期間を入力する。
func TestLibWorkflow(t *testing.T) {
	svc := &fakeServices{calilLibrary: calilFixture(t, "library_setagaya.json")}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "lib", "find", "--pref", "東京都", "--city", "世田谷区", "--name", "区立"); code != ExitOK {
		t.Fatalf("find: exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "lib_find.golden", stdout.String())
	q := svc.recorded()[0].url.Query()
	if q.Get("pref") != "東京都" || q.Get("city") != "世田谷区" || q.Get("appkey") != testAppKey || q.Has("name") {
		t.Errorf("find の要求 = %v", q)
	}

	for _, id := range []string{"Tokyo_Setagaya:中央", "Tokyo_Setagaya:経堂"} {
		if code := run(env, stdout, stderr, "lib", "add", id); code != ExitOK {
			t.Fatalf("add %s: exit code = %d, stderr = %s", id, code, stderr)
		}
	}
	if !strings.Contains(stdout.String(), "世田谷区立経堂図書館") {
		t.Errorf("add の出力 = %q", stdout)
	}
	if q := svc.recorded()[1].url.Query(); q.Get("systemid") != "Tokyo_Setagaya" || q.Get("appkey") != testAppKey {
		t.Errorf("add の要求 = %v", q)
	}
	lib := findLibrary(t, env, "Tokyo_Setagaya:中央")
	if lib.Name != "世田谷区立中央図書館" || lib.Pref != "東京都" || lib.City != "世田谷区" || lib.Schedule != nil {
		t.Errorf("登録された図書館 = %+v", lib)
	}

	if code := run(env, stdout, stderr, "lib", "hours", "中央", "--sun", "09:00-17:00", "--mon", "closed", "--wed", "09:00-19:00"); code != ExitOK {
		t.Fatalf("hours: exit code = %d, stderr = %s", code, stderr)
	}
	s := findLibrary(t, env, "Tokyo_Setagaya:中央").Schedule
	if s == nil {
		t.Fatal("開館スケジュールが保存されていない")
	}
	for wd, want := range map[time.Weekday]string{time.Sunday: "09:00-17:00", time.Monday: "closed", time.Tuesday: "closed", time.Wednesday: "09:00-19:00", time.Saturday: "closed"} {
		if got := dayText(s.Weekly[wd]); got != want {
			t.Errorf("%s = %s, want %s", wd, got, want)
		}
	}
	if s.Holiday.Mode() != domain.HolidaySameAsWeekday || s.UpdatedOn.String() != "2026-10-21" {
		t.Errorf("祝日 = %v, 最終更新日 = %s", s.Holiday.Mode(), s.UpdatedOn)
	}

	// 指定しなかった曜日と祝日の扱いは変わらない。
	if code := run(env, stdout, stderr, "lib", "hours", "Tokyo_Setagaya:中央", "--holiday", "closed"); code != ExitOK {
		t.Fatalf("hours --holiday: exit code = %d, stderr = %s", code, stderr)
	}
	if code := run(env, stdout, stderr, "lib", "hours", "中央", "--wed", "10:00-18:00"); code != ExitOK {
		t.Fatalf("hours --wed: exit code = %d, stderr = %s", code, stderr)
	}
	s = findLibrary(t, env, "Tokyo_Setagaya:中央").Schedule
	if got := dayText(s.Weekly[time.Sunday]); got != "09:00-17:00" {
		t.Errorf("日曜 = %s, want 09:00-17:00（変わらない）", got)
	}
	if got := dayText(s.Weekly[time.Wednesday]); got != "10:00-18:00" {
		t.Errorf("水曜 = %s, want 10:00-18:00", got)
	}
	if s.Holiday.Mode() != domain.HolidayClosed {
		t.Errorf("祝日 = %v, want 休館（変わらない）", s.Holiday.Mode())
	}

	if code := run(env, stdout, stderr, "lib", "closure", "add", "中央", "2026-11-10", "2026-11-14", "--reason", "特別整理期間"); code != ExitOK {
		t.Fatalf("closure add: exit code = %d, stderr = %s", code, stderr)
	}
	if code := run(env, stdout, stderr, "lib", "closure", "add", "中央", "--reason", "臨時休館", "2026-10-21", "2026-10-21"); code != ExitOK {
		t.Fatalf("closure add: exit code = %d, stderr = %s", code, stderr)
	}
	if code := run(env, stdout, stderr, "lib", "closure", "list", "中央"); code != ExitOK {
		t.Fatalf("closure list: exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "lib_closure_list.golden", stdout.String())

	if code := run(env, stdout, stderr, "lib", "list"); code != ExitOK {
		t.Fatalf("list: exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "lib_list.golden", stdout.String())

	if code := run(env, stdout, stderr, "lib", "closure", "rm", "中央", "1"); code != ExitOK {
		t.Fatalf("closure rm: exit code = %d, stderr = %s", code, stderr)
	}
	closures := findLibrary(t, env, "Tokyo_Setagaya:中央").Closures
	if len(closures) != 1 || closures[0].Start().String() != "2026-11-10" {
		t.Errorf("closure rm 後の期間 = %+v", closures)
	}

	for _, r := range svc.recorded() {
		if r.host != "api.calil.jp" || r.url.Path != "/library" {
			t.Errorf("図書館検索以外に要求した: %s%s", r.host, r.url.Path)
		}
		if r.agent != userAgent {
			t.Errorf("User-Agent = %q", r.agent)
		}
	}
	if n := len(svc.recorded()); n != 3 {
		t.Errorf("要求 = %d, want 3（find と add 2回）", n)
	}
}

// US3 受け入れシナリオ 1〜3: CLI で登録・削除した図書館と入力した開館情報が check の対象と判定に反映される。
func TestLibChangesCheckTargets(t *testing.T) {
	svc := &fakeServices{calilLibrary: calilFixture(t, "library_setagaya.json")}
	env, stdout, stderr := newCheckEnv(t, "", svc)
	if err := os.WriteFile(filepath.Join(env.DataDir, "data.json"), []byte(`{"version": 1, "books": [{"isbn": "9784003101018", "title": "本のタイトルP", "addedOn": "2026-10-01"}], "libraries": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHoldingsCache(t, env, `
	  "9784003101018|Tokyo_Setagaya": {"isbn": "9784003101018", "systemId": "Tokyo_Setagaya", "systemStatus": "OK", "byLibKey": {"中央": "貸出可", "経堂": "貸出可", "奥沢": "貸出可"}, "fetchedAt": "2026-10-21T13:50:00+09:00"}`)

	for _, args := range [][]string{
		{"lib", "add", "Tokyo_Setagaya:中央"},
		{"lib", "add", "Tokyo_Setagaya:経堂"},
		{"lib", "add", "Tokyo_Setagaya:奥沢"},
		{"lib", "hours", "中央", "--wed", "09:00-19:00"},
		{"lib", "hours", "経堂", "--wed", "09:00-20:00"},
	} {
		if code := run(env, stdout, stderr, args...); code != ExitOK {
			t.Fatalf("%v: exit code = %d, stderr = %s", args, code, stderr)
		}
	}

	if code := run(env, stdout, stderr, "check"); code != ExitOK {
		t.Fatalf("check: exit code = %d, stderr = %s", code, stderr)
	}
	for _, want := range []string{"1. 世田谷区立経堂図書館  開館中 〜20:00", "2. 世田谷区立中央図書館  開館中 〜19:00", "- 世田谷区立奥沢図書館  開館情報未入力"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("出力に %q がない:\n%s", want, stdout)
		}
	}

	// 削除した館は対象外になり、今日を含む臨時休館期間を入力した館は休館になる。
	for _, args := range [][]string{
		{"lib", "rm", "奥沢"},
		{"lib", "closure", "add", "経堂", "2026-10-20", "2026-10-24", "--reason", "特別整理期間"},
	} {
		if code := run(env, stdout, stderr, args...); code != ExitOK {
			t.Fatalf("%v: exit code = %d, stderr = %s", args, code, stderr)
		}
	}
	if code := run(env, stdout, stderr, "check"); code != ExitOK {
		t.Fatalf("check: exit code = %d, stderr = %s", code, stderr)
	}
	if strings.Contains(stdout.String(), "奥沢") {
		t.Errorf("削除した館が表示された:\n%s", stdout)
	}
	for _, want := range []string{"1. 世田谷区立中央図書館  開館中 〜19:00", "- 世田谷区立経堂図書館  休館（特別整理期間 2026-10-20〜2026-10-24）"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("出力に %q がない:\n%s", want, stdout)
		}
	}
}

func TestLibFindErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		appKey string
		want   int
		stderr string
		sent   bool
	}{
		{name: "都道府県なし", args: []string{"lib", "find", "--city", "世田谷区"}, appKey: testAppKey, want: ExitUsage, stderr: "--pref"},
		{name: "余分な引数", args: []string{"lib", "find", "--pref", "東京都", "中央"}, appKey: testAppKey, want: ExitUsage, stderr: "使い方"},
		{name: "名前に一致しない", args: []string{"lib", "find", "--pref", "東京都", "--name", "存在しない館"}, appKey: testAppKey, want: ExitUsage, stderr: "「存在しない館」を含む図書館は見つかりませんでした", sent: true},
		{name: "キーなし", args: []string{"lib", "find", "--pref", "東京都"}, want: ExitError, stderr: "LIBREAD_CALIL_APPKEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeServices{calilLibrary: calilFixture(t, "library_setagaya.json")}
			env, stdout, stderr := newCheckEnv(t, "", svc)
			env.CalilAppKey = tt.appKey
			if code := run(env, stdout, stderr, tt.args...); code != tt.want {
				t.Errorf("exit code = %d, want %d", code, tt.want)
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want %q を含む", stderr, tt.stderr)
			}
			if sent := len(svc.recorded()) > 0; sent != tt.sent {
				t.Errorf("要求を送った = %v, want %v", sent, tt.sent)
			}
		})
	}
}

func TestLibFindWithoutNameShowsAll(t *testing.T) {
	svc := &fakeServices{calilLibrary: calilFixture(t, "library_setagaya.json")}
	env, stdout, stderr := newCheckEnv(t, "", svc)
	if code := run(env, stdout, stderr, "lib", "find", "--pref", "東京都"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "「東京都」の図書館（4館）") || !strings.Contains(stdout.String(), "Univ_Komazawa:図書館") {
		t.Errorf("出力 = %s", stdout)
	}
	if svc.recorded()[0].url.Query().Has("city") {
		t.Errorf("city を送った")
	}
}

func TestLibAddErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stderr string
		sent   bool
	}{
		{name: "重複", args: []string{"lib", "add", "Tokyo_Setagaya:中央"}, stderr: "すでに利用図書館に登録されています"},
		{name: "形式の誤り", args: []string{"lib", "add", "世田谷区立中央図書館"}, stderr: "SystemID:LibKey"},
		{name: "引数なし", args: []string{"lib", "add"}, stderr: "使い方"},
		{name: "未知の館", args: []string{"lib", "add", "Tokyo_Setagaya:存在しない館"}, stderr: "カーリルに見つかりませんでした", sent: true},
		{name: "未知のシステム", args: []string{"lib", "add", "No_Such:中央"}, stderr: "カーリルに見つかりませんでした", sent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeServices{calilLibrary: calilFixture(t, "library_setagaya.json")}
			env, stdout, stderr := newCheckEnv(t, "", svc)
			if code := run(env, stdout, stderr, "lib", "add", "Tokyo_Setagaya:中央"); code != ExitOK {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr)
			}
			before := len(svc.recorded())
			if tt.name == "未知のシステム" {
				svc.calilLibrary = "[]"
			}
			if code := run(env, stdout, stderr, tt.args...); code != ExitUsage {
				t.Errorf("exit code = %d, want %d", code, ExitUsage)
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want %q を含む", stderr, tt.stderr)
			}
			if sent := len(svc.recorded()) > before; sent != tt.sent {
				t.Errorf("要求を送った = %v, want %v", sent, tt.sent)
			}
			if n := len(loadData(t, env).Libraries); n != 1 {
				t.Errorf("利用図書館 = %d, want 1", n)
			}
		})
	}
}

func TestLibAddRequiresAppKey(t *testing.T) {
	svc := &fakeServices{calilLibrary: calilFixture(t, "library_setagaya.json")}
	env, stdout, stderr := newCheckEnv(t, "", svc)
	env.CalilAppKey = ""
	if code := run(env, stdout, stderr, "lib", "add", "Tokyo_Setagaya:中央"); code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "LIBREAD_CALIL_APPKEY") || len(svc.recorded()) != 0 {
		t.Errorf("stderr = %q, 要求 = %d", stderr, len(svc.recorded()))
	}
}

// registerLibraries は data.json に開館スケジュールのない利用図書館を置く。
func registerLibraries(t *testing.T, env Env) {
	t.Helper()
	raw := `{"version": 1, "books": [], "libraries": [
	  {"systemId": "Tokyo_Setagaya", "libKey": "中央", "name": "世田谷区立中央図書館", "pref": "東京都", "city": "世田谷区", "closures": []},
	  {"systemId": "Tokyo_Setagaya", "libKey": "経堂", "name": "世田谷区立経堂図書館", "pref": "東京都", "city": "世田谷区", "closures": []}]}`
	if err := os.WriteFile(filepath.Join(env.DataDir, "data.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLibHoursNewSchedule(t *testing.T) {
	env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
	registerLibraries(t, env)
	if code := run(env, stdout, stderr, "lib", "hours", "経堂", "--sat", "10:00-17:00", "--holiday", "09:30-16:00"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "lib_hours.golden", stdout.String())
	s := findLibrary(t, env, "Tokyo_Setagaya:経堂").Schedule
	for wd := time.Sunday; wd <= time.Friday; wd++ {
		if got := dayText(s.Weekly[wd]); got != "closed" {
			t.Errorf("%s = %s, want closed（指定しなかった曜日）", wd, got)
		}
	}
	if h, ok := s.Holiday.CustomHours(); !ok || h.String() != "09:30-16:00" {
		t.Errorf("祝日 = %v %v", h, ok)
	}
	if findLibrary(t, env, "Tokyo_Setagaya:中央").Schedule != nil {
		t.Error("指定していない館のスケジュールが変わった")
	}
}

func TestLibHoursErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{name: "指定なし", args: []string{"lib", "hours", "中央"}, stderr: "--sun"},
		{name: "図書館なし", args: []string{"lib", "hours", "--sun", "09:00-17:00"}, stderr: "使い方"},
		{name: "時刻の誤り", args: []string{"lib", "hours", "中央", "--sun", "9:00-17:00"}, stderr: "HH:MM"},
		{name: "区切りなし", args: []string{"lib", "hours", "中央", "--sun", "09:00"}, stderr: "HH:MM-HH:MM"},
		{name: "開館が閉館より後", args: []string{"lib", "hours", "中央", "--mon", "18:00-09:00"}, stderr: "より前"},
		{name: "祝日の誤り", args: []string{"lib", "hours", "中央", "--holiday", "weekday"}, stderr: "same"},
		{name: "未登録の館", args: []string{"lib", "hours", "梅丘", "--sun", "09:00-17:00"}, stderr: "一致する利用図書館はありません"},
		{name: "複数一致", args: []string{"lib", "hours", "世田谷区立", "--sun", "09:00-17:00"}, stderr: "複数あります"},
		{name: "不明なオプション", args: []string{"lib", "hours", "中央", "--sunday", "09:00-17:00"}, stderr: "使い方"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
			registerLibraries(t, env)
			if code := run(env, stdout, stderr, tt.args...); code != ExitUsage {
				t.Errorf("exit code = %d, want %d", code, ExitUsage)
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want %q を含む", stderr, tt.stderr)
			}
			if findLibrary(t, env, "Tokyo_Setagaya:中央").Schedule != nil {
				t.Error("誤った指定でスケジュールが保存された")
			}
		})
	}
}

func TestLibClosureErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{name: "開始日が終了日より後", args: []string{"lib", "closure", "add", "中央", "2026-11-14", "2026-11-10"}, stderr: "開始日は終了日以前"},
		{name: "日付の誤り", args: []string{"lib", "closure", "add", "中央", "2026/11/10", "2026-11-14"}, stderr: "YYYY-MM-DD"},
		{name: "終了日なし", args: []string{"lib", "closure", "add", "中央", "2026-11-10"}, stderr: "使い方"},
		{name: "番号が範囲外", args: []string{"lib", "closure", "rm", "中央", "2"}, stderr: "番号"},
		{name: "番号が0", args: []string{"lib", "closure", "rm", "中央", "0"}, stderr: "番号"},
		{name: "番号が数でない", args: []string{"lib", "closure", "rm", "中央", "最初"}, stderr: "番号"},
		{name: "list の図書館なし", args: []string{"lib", "closure", "list"}, stderr: "使い方"},
		{name: "不明なサブコマンド", args: []string{"lib", "closure", "edit", "中央"}, stderr: "使い方"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
			registerLibraries(t, env)
			if code := run(env, stdout, stderr, "lib", "closure", "add", "中央", "2026-11-10", "2026-11-14"); code != ExitOK {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr)
			}
			if code := run(env, stdout, stderr, tt.args...); code != ExitUsage {
				t.Errorf("exit code = %d, want %d", code, ExitUsage)
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want %q を含む", stderr, tt.stderr)
			}
			if n := len(findLibrary(t, env, "Tokyo_Setagaya:中央").Closures); n != 1 {
				t.Errorf("臨時休館期間 = %d, want 1", n)
			}
		})
	}
}

func TestLibClosureListEmpty(t *testing.T) {
	env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
	registerLibraries(t, env)
	if code := run(env, stdout, stderr, "lib", "closure", "list", "中央"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "世田谷区立中央図書館の臨時休館期間は登録されていません") {
		t.Errorf("出力 = %q", stdout)
	}
}

// US3 受け入れシナリオ 2: 削除した館は利用図書館から外れる。
func TestLibRm(t *testing.T) {
	env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
	registerLibraries(t, env)
	if code := run(env, stdout, stderr, "lib", "rm", "世田谷区立"); code != ExitUsage {
		t.Errorf("複数一致: exit code = %d, want %d", code, ExitUsage)
	}
	if code := run(env, stdout, stderr, "lib", "rm", "Tokyo_Setagaya:経堂"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "利用図書館から外しました: 世田谷区立経堂図書館（Tokyo_Setagaya:経堂）") {
		t.Errorf("出力 = %q", stdout)
	}
	libs := loadData(t, env).Libraries
	if len(libs) != 1 || libs[0].ID.LibKey != "中央" {
		t.Errorf("rm 後の利用図書館 = %+v", libs)
	}
	if code := run(env, stdout, stderr, "lib", "rm", "経堂"); code != ExitUsage {
		t.Errorf("外した館: exit code = %d, want %d", code, ExitUsage)
	}
}

func TestLibListEmpty(t *testing.T) {
	env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
	if code := run(env, stdout, stderr, "lib", "list"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "libread lib find --pref") {
		t.Errorf("出力 = %q", stdout)
	}
}

func TestLibUsage(t *testing.T) {
	for _, args := range [][]string{{"lib"}, {"lib", "edit"}, {"lib", "list", "余分"}, {"lib", "rm"}} {
		env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
		if code := run(env, stdout, stderr, args...); code != ExitUsage {
			t.Errorf("%v: exit code = %d, want %d", args, code, ExitUsage)
		}
		if !strings.Contains(stderr.String(), "使い方") {
			t.Errorf("%v: stderr = %q", args, stderr)
		}
	}
}
