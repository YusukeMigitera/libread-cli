package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"

	"libread/internal/checkrun"
	"libread/internal/domain"
	"libread/internal/holiday"
	"libread/internal/store"
)

// version は User-Agent に付ける libread のバージョン（research.md R9）。
const version = "0.1.0"

const userAgent = "libread/" + version

var checkCommand = command{
	name:    "check",
	summary: "今日読める図書館を表示する（引数なしの libread と同じ）。check <本> でその本だけを調べる",
	run:     runCheck,
}

func runCheck(ctx context.Context, args []string, env Env) error {
	if slices.ContainsFunc(args, isHelpFlag) {
		return &helpRequest{usage: "使い方: libread check [<本>]"}
	}
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageErrorf("使い方: libread check [<本>]")
	}
	if fs.NArg() > 1 {
		return usageErrorf("使い方: libread check [<本>]")
	}

	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	if len(data.Libraries) == 0 {
		return usageErrorf("利用図書館が登録されていません。libread lib find --pref <都道府県> で図書館を探し、libread lib add で登録してください")
	}
	if len(data.Books) == 0 {
		return usageErrorf("読みかけの本が登録されていません。libread book find <書名> で本を探し、libread book add で登録してください")
	}
	books := data.Books
	if fs.NArg() == 1 {
		b, err := resolveBook(data.Books, fs.Arg(0))
		if err != nil {
			return err
		}
		books = []domain.ReadingBook{b}
	}
	if err := requireAppKey(env); err != nil {
		return err
	}

	now := env.Now()
	cache := store.NewCache(env.CacheDir)
	deps := checkrun.Deps{
		Calil: calilClient(env),
		Holidays: &holiday.Client{
			HTTP:      env.HTTPClient,
			Cache:     cache,
			Now:       env.Now,
			UserAgent: userAgent,
		},
		Cache: cache,
		Warn:  func(msg string) { fmt.Fprintf(env.Stderr, "libread: 警告: %s\n", msg) },
	}
	result, err := checkrun.Run(ctx, deps, data, books, now)
	if err != nil {
		return err
	}
	writeResult(env.Stdout, result, books, data.Libraries)
	return nil
}
