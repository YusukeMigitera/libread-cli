package cli

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"

	"libread/internal/domain"
	"libread/internal/store"
)

const closureUsage = `使い方:
  libread lib closure add <図書館> <開始日> <終了日> [--reason <テキスト>]
  libread lib closure list <図書館>
  libread lib closure rm <図書館> <番号>`

// runLibClosure は臨時休館期間を追加・表示・削除する（FR-016）。
func runLibClosure(args []string, env Env) error {
	if len(args) == 0 {
		return usageErrorf("%s", closureUsage)
	}
	switch args[0] {
	case "add":
		return runClosureAdd(args[1:], env)
	case "list":
		return runClosureList(args[1:], env)
	case "rm":
		return runClosureRm(args[1:], env)
	}
	return usageErrorf("不明なコマンドです: lib closure %s\n%s", args[0], closureUsage)
}

// runClosureAdd は期間を追加する。期間は開始日の順に並べておき、list と rm の番号がその順になるようにする。
func runClosureAdd(args []string, env Env) error {
	fs := flag.NewFlagSet("lib closure add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reason := fs.String("reason", "", "休館の理由")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 3 {
		return usageErrorf("使い方: libread lib closure add <図書館> <開始日> <終了日> [--reason <テキスト>]（日付は YYYY-MM-DD）")
	}
	start, err := domain.ParseDate(pos[1])
	if err != nil {
		return usageErrorf("開始日: %v", err)
	}
	end, err := domain.ParseDate(pos[2])
	if err != nil {
		return usageErrorf("終了日: %v", err)
	}
	period, err := domain.NewClosurePeriod(start, end, *reason)
	if err != nil {
		return usageErrorf("%v", err)
	}

	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	target, err := resolveLibrary(data.Libraries, pos[0])
	if err != nil {
		return err
	}
	closures := append(slices.Clone(target.Closures), period)
	slices.SortStableFunc(closures, func(a, b domain.ClosurePeriod) int {
		switch {
		case a.Start().Before(b.Start()):
			return -1
		case a.Start().After(b.Start()):
			return 1
		}
		return 0
	})
	setClosures(&data, target.ID, closures)
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "臨時休館期間を追加しました: %s %s\n", target.Name, describeClosure(period))
	return nil
}

// runClosureList は期間を番号付きで表示する。番号は closure rm で使う。
func runClosureList(args []string, env Env) error {
	if len(args) != 1 {
		return usageErrorf("使い方: libread lib closure list <図書館>")
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	target, err := resolveLibrary(data.Libraries, args[0])
	if err != nil {
		return err
	}
	if len(target.Closures) == 0 {
		fmt.Fprintf(env.Stdout, "%sの臨時休館期間は登録されていません\n", target.Name)
		return nil
	}
	fmt.Fprintf(env.Stdout, "%sの臨時休館期間（%d件）\n", target.Name, len(target.Closures))
	for i, c := range target.Closures {
		fmt.Fprintf(env.Stdout, "%d. %s\n", i+1, describeClosure(c))
	}
	return nil
}

// runClosureRm は closure list の番号で期間を削除する。
func runClosureRm(args []string, env Env) error {
	if len(args) != 2 {
		return usageErrorf("使い方: libread lib closure rm <図書館> <番号>（番号は libread lib closure list で確認できます）")
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	target, err := resolveLibrary(data.Libraries, args[0])
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 1 || n > len(target.Closures) {
		return usageErrorf("番号 %q: libread lib closure list %s で表示される番号（1〜%d）を指定してください", args[1], args[0], len(target.Closures))
	}
	removed := target.Closures[n-1]
	setClosures(&data, target.ID, slices.Delete(slices.Clone(target.Closures), n-1, n))
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "臨時休館期間を削除しました: %s %s\n", target.Name, describeClosure(removed))
	return nil
}

func setClosures(data *store.Data, id domain.LibraryID, closures []domain.ClosurePeriod) {
	for i := range data.Libraries {
		if data.Libraries[i].ID == id {
			data.Libraries[i].Closures = closures
		}
	}
}
