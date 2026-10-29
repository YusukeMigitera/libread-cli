package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"libread/internal/calil"
	"libread/internal/domain"
	"libread/internal/store"
)

// calilAttribution はカーリルの利用条件で必須のリンク表示（research.md R1）。
const calilAttribution = "図書館情報: カーリル（https://calil.jp/）"

const libUsage = `使い方:
  libread lib find --pref <都道府県> [--city <市区町村>] [--name <部分一致>]
  libread lib add <SystemID:LibKey>
  libread lib list
  libread lib rm <図書館>
  libread lib hours <図書館> [--sun 〜 --sat <HH:MM-HH:MM|closed>] [--holiday <same|closed|HH:MM-HH:MM>]
  libread lib closure add <図書館> <開始日> <終了日> [--reason <テキスト>]
  libread lib closure list <図書館>
  libread lib closure rm <図書館> <番号>`

const libHoursUsage = "使い方: libread lib hours <図書館> [--sun 〜 --sat <HH:MM-HH:MM|closed>] [--holiday <same|closed|HH:MM-HH:MM>]"

var libCommand = command{
	name:    "lib",
	summary: "利用図書館を探す・登録する（find / add / list / rm / hours / closure）",
	run:     runLib,
}

func runLib(ctx context.Context, args []string, env Env) error {
	if len(args) == 0 {
		return usageErrorf("%s", libUsage)
	}
	switch args[0] {
	case "find":
		return runLibFind(ctx, args[1:], env)
	case "add":
		return runLibAdd(ctx, args[1:], env)
	case "list":
		return runLibList(args[1:], env)
	case "rm":
		return runLibRm(args[1:], env)
	case "hours":
		return runLibHours(args[1:], env)
	case "closure":
		return runLibClosure(args[1:], env)
	}
	return usageErrorf("不明なコマンドです: lib %s\n%s", args[0], libUsage)
}

// runLibFind は都道府県（必須）と市区町村でカーリルの図書館を探し、名前の部分一致で絞って表示する（FR-011）。
func runLibFind(ctx context.Context, args []string, env Env) error {
	const usage = "使い方: libread lib find --pref <都道府県> [--city <市区町村>] [--name <部分一致>]"
	fs := flag.NewFlagSet("lib find", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pref := fs.String("pref", "", "都道府県")
	city := fs.String("city", "", "市区町村")
	name := fs.String("name", "", "図書館名の部分一致")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 0 {
		return usageErrorf("%s", usage)
	}
	p, c, n := strings.TrimSpace(*pref), strings.TrimSpace(*city), strings.TrimSpace(*name)
	if p == "" {
		return usageErrorf("都道府県を --pref で指定してください（例: --pref 東京都）\n%s", usage)
	}
	if err := requireAppKey(env); err != nil {
		return err
	}
	found, err := calilClient(env).Libraries(ctx, p, c)
	if err != nil {
		return err
	}
	area := strings.TrimSpace(p + " " + c)
	var libs []calil.LibraryInfo
	for _, l := range found {
		if strings.Contains(l.Formal, n) {
			libs = append(libs, l)
		}
	}
	if len(libs) == 0 {
		fmt.Fprintln(env.Stdout, calilAttribution)
		if n != "" {
			return usageErrorf("「%s」で「%s」を含む図書館は見つかりませんでした", area, n)
		}
		return usageErrorf("「%s」の図書館は見つかりませんでした。都道府県・市区町村の表記（例: 東京都、世田谷区）を確かめてください", area)
	}
	if n != "" {
		fmt.Fprintf(env.Stdout, "「%s」で「%s」を含む図書館（%d館）\n", area, n, len(libs))
	} else {
		fmt.Fprintf(env.Stdout, "「%s」の図書館（%d館）\n", area, len(libs))
	}
	for i, l := range libs {
		fmt.Fprintf(env.Stdout, "%d. %s  %s  %s  %s\n", i+1, l.ID, l.Formal, l.City, l.Address)
	}
	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, "登録するには: libread lib add <SystemID:LibKey>")
	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, calilAttribution)
	return nil
}

// runLibAdd は SystemID:LibKey の図書館をカーリルで確かめ、利用図書館として登録する（FR-011）。
func runLibAdd(ctx context.Context, args []string, env Env) error {
	if len(args) != 1 {
		return usageErrorf("使い方: libread lib add <SystemID:LibKey>（libread lib find で表示される ID）")
	}
	id, err := domain.ParseLibraryID(args[0])
	if err != nil {
		return usageErrorf("%v。図書館は libread lib find --pref <都道府県> で探してください", err)
	}
	if err := requireAppKey(env); err != nil {
		return err
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	for _, l := range data.Libraries {
		if l.ID == id {
			return usageErrorf("%s（%s）はすでに利用図書館に登録されています", l.Name, id)
		}
	}
	found, err := calilClient(env).LibrariesOfSystem(ctx, id.SystemID)
	if err != nil {
		return err
	}
	var info *calil.LibraryInfo
	for i := range found {
		if found[i].ID == id {
			info = &found[i]
			break
		}
	}
	if info == nil {
		return usageErrorf("図書館 %s はカーリルに見つかりませんでした。libread lib find --pref <都道府県> で表示される ID を指定してください", id)
	}
	data.Libraries = append(data.Libraries, domain.RegisteredLibrary{
		ID:       id,
		Name:     info.Formal,
		Pref:     info.Pref,
		City:     info.City,
		Closures: []domain.ClosurePeriod{},
	})
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "利用図書館に登録しました: %s（%s）\n", info.Formal, id)
	fmt.Fprintln(env.Stdout, "開館スケジュールを入力するには: libread lib hours <図書館> --sun 09:00-17:00 --mon closed ...")
	return nil
}

// runLibList は利用図書館と開館スケジュールの概要を登録順に表示する。
func runLibList(args []string, env Env) error {
	if len(args) != 0 {
		return usageErrorf("使い方: libread lib list")
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	if len(data.Libraries) == 0 {
		fmt.Fprintln(env.Stdout, "利用図書館は登録されていません。libread lib find --pref <都道府県> で図書館を探し、libread lib add で登録してください")
		return nil
	}
	fmt.Fprintf(env.Stdout, "利用図書館（%d館）\n", len(data.Libraries))
	for i, l := range data.Libraries {
		fmt.Fprintf(env.Stdout, "%d. %s（%s）", i+1, l.Name, l.ID)
		if area := strings.TrimSpace(l.Pref + " " + l.City); area != "" {
			fmt.Fprintf(env.Stdout, " %s", area)
		}
		fmt.Fprintln(env.Stdout)
		writeSchedule(env.Stdout, l.Schedule)
		if len(l.Closures) > 0 {
			periods := make([]string, len(l.Closures))
			for j, c := range l.Closures {
				periods[j] = describeClosure(c)
			}
			fmt.Fprintf(env.Stdout, "   臨時休館: %s\n", strings.Join(periods, " / "))
		}
	}
	return nil
}

// writeSchedule は開館スケジュールを曜日ごとに1行で書く。
func writeSchedule(w io.Writer, s *domain.Schedule) {
	if s == nil {
		fmt.Fprintln(w, "   開館情報未入力（libread lib hours で入力してください）")
		return
	}
	days := make([]string, 0, len(s.Weekly)+1)
	for i, rule := range s.Weekly {
		days = append(days, weekdayNames[i]+" "+dayRuleText(rule))
	}
	days = append(days, "祝日 "+holidayRuleText(s.Holiday))
	fmt.Fprintf(w, "   開館: %s\n", strings.Join(days, " / "))
	fmt.Fprintf(w, "   最終更新: %s\n", s.UpdatedOn)
}

func dayRuleText(rule domain.DayRule) string {
	if h, open := rule.Hours(); open {
		return h.String()
	}
	return "休館"
}

func holidayRuleText(r domain.HolidayRule) string {
	switch r.Mode() {
	case domain.HolidayClosed:
		return "休館"
	case domain.HolidayCustom:
		h, _ := r.CustomHours()
		return h.String()
	default:
		return "曜日どおり"
	}
}

// runLibRm は利用図書館から外す（US3 受け入れシナリオ 2）。
func runLibRm(args []string, env Env) error {
	if len(args) != 1 {
		return usageErrorf("使い方: libread lib rm <図書館>")
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	target, err := resolveLibrary(data.Libraries, args[0])
	if err != nil {
		return err
	}
	kept := data.Libraries[:0]
	for _, l := range data.Libraries {
		if l.ID != target.ID {
			kept = append(kept, l)
		}
	}
	data.Libraries = kept
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "利用図書館から外しました: %s（%s）\n", target.Name, target.ID)
	return nil
}

var weekdayFlags = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// runLibHours は開館スケジュールを設定する（FR-016、contracts/cli.md「lib hours の値」）。
// 指定しなかった曜日は現在の値のまま、スケジュールがまだ無ければ休館にする。
func runLibHours(args []string, env Env) error {
	fs := flag.NewFlagSet("lib hours", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var days [7]*string
	for i, name := range weekdayFlags {
		days[i] = fs.String(name, "", weekdayNames[i]+"曜日の開館時間（HH:MM-HH:MM または closed）")
	}
	holidayFlag := fs.String("holiday", "", "祝日の扱い（same / closed / HH:MM-HH:MM）")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return usageErrorf("%s", libHoursUsage)
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if len(given) == 0 {
		return usageErrorf("--sun 〜 --sat または --holiday で開館時間を指定してください\n%s", libHoursUsage)
	}

	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	target, err := resolveLibrary(data.Libraries, pos[0])
	if err != nil {
		return err
	}
	var s domain.Schedule // 新しく作るときは、全曜日が休館（ゼロ値）で祝日は曜日どおり
	if target.Schedule != nil {
		s = *target.Schedule
	} else if !given["sun"] && !given["mon"] && !given["tue"] && !given["wed"] && !given["thu"] && !given["fri"] && !given["sat"] {
		// 曜日を指定せずに作ると全曜日が休館になり、「開館情報未入力」だった館が毎日休館と判定されてしまう。
		return usageErrorf("%sの開館スケジュールはまだありません。初めて設定するときは --sun 〜 --sat で曜日の開館時間を1つ以上指定してください\n%s", target.Name, libHoursUsage)
	}
	for i, name := range weekdayFlags {
		if !given[name] {
			continue
		}
		rule, err := parseDayRule(*days[i])
		if err != nil {
			return usageErrorf("--%s: %v", name, err)
		}
		s.Weekly[i] = rule
	}
	if given["holiday"] {
		rule, err := parseHolidayRule(*holidayFlag)
		if err != nil {
			return usageErrorf("--holiday: %v", err)
		}
		s.Holiday = rule
	}
	s.UpdatedOn = domain.DateOf(env.Now())

	for i := range data.Libraries {
		if data.Libraries[i].ID == target.ID {
			data.Libraries[i].Schedule = &s
		}
	}
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "開館スケジュールを設定しました: %s（%s）\n", target.Name, target.ID)
	writeSchedule(env.Stdout, &s)
	return nil
}

// parseDayRule は "HH:MM-HH:MM" または "closed" を読む。
func parseDayRule(v string) (domain.DayRule, error) {
	if v == "closed" {
		return domain.DayRule{}, nil
	}
	h, err := parseHoursRange(v)
	if err != nil {
		return domain.DayRule{}, err
	}
	return domain.OpenDay(h), nil
}

// parseHolidayRule は "same"、"closed" または "HH:MM-HH:MM" を読む。
func parseHolidayRule(v string) (domain.HolidayRule, error) {
	switch v {
	case "same":
		return domain.HolidaySameAsWeekdayRule(), nil
	case "closed":
		return domain.HolidayClosedRule(), nil
	}
	h, err := parseHoursRange(v)
	if err != nil {
		return domain.HolidayRule{}, fmt.Errorf("same / closed / HH:MM-HH:MM のどれかで指定してください（%v）", err)
	}
	return domain.HolidayCustomHours(h), nil
}

func parseHoursRange(v string) (domain.Hours, error) {
	open, close, ok := strings.Cut(v, "-")
	if !ok {
		return domain.Hours{}, fmt.Errorf("%q: HH:MM-HH:MM 形式（例: 09:00-17:00）または closed で指定してください", v)
	}
	o, err := domain.ParseTimeOfDay(open)
	if err != nil {
		return domain.Hours{}, err
	}
	c, err := domain.ParseTimeOfDay(close)
	if err != nil {
		return domain.Hours{}, err
	}
	return domain.NewHours(o, c)
}

// describeClosure は 開始日〜終了日（理由）の形で臨時休館期間を表す。
func describeClosure(c domain.ClosurePeriod) string {
	s := c.Start().String() + "〜" + c.End().String()
	if c.Reason() != "" {
		s += "（" + c.Reason() + "）"
	}
	return s
}
