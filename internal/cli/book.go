package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"libread/internal/domain"
	"libread/internal/ndl"
	"libread/internal/store"
)

// ndlAttribution は NDL サーチの利用条件で必須の出典表示（contracts/cli.md、research.md R10）。
const ndlAttribution = "書誌情報: 国立国会図書館サーチ API を利用しています（https://ndlsearch.ndl.go.jp/）"

const bookUsage = `使い方:
  libread book find <書名|ISBN> [--author <著者>] [--publisher <出版社>]
  libread book add <ISBN> [--note <テキスト>]
  libread book list
  libread book note <本> <テキスト>
  libread book rm <本>`

var bookCommand = command{
	name:    "book",
	summary: "読みかけの本を探す・登録する（find / add / list / note / rm）",
	run:     runBook,
}

func runBook(ctx context.Context, args []string, env Env) error {
	if len(args) == 0 {
		return usageErrorf("%s", bookUsage)
	}
	switch args[0] {
	case "find":
		return runBookFind(ctx, args[1:], env)
	case "add":
		return runBookAdd(ctx, args[1:], env)
	case "list":
		return runBookList(args[1:], env)
	case "note":
		return runBookNote(args[1:], env)
	case "rm":
		return runBookRm(args[1:], env)
	}
	return usageErrorf("不明なコマンドです: book %s\n%s", args[0], bookUsage)
}

// ndlTimeout は NDL サーチへの要求のタイムアウト。NDL サーチは同じ要求でも応答が数十秒かかることがあるので、
// 他のサービスより長くする（research.md R3）。
const ndlTimeout = 30 * time.Second

// ndlClient は env.HTTPClient の写しのタイムアウトだけを ndlTimeout にして使う。
func ndlClient(env Env) *ndl.Client {
	hc := *env.HTTPClient
	hc.Timeout = ndlTimeout
	return &ndl.Client{HTTP: &hc, UserAgent: userAgent}
}

// runBookFind は書名または ISBN で書誌を探し、候補を表示する（FR-009）。
func runBookFind(ctx context.Context, args []string, env Env) error {
	const usage = "使い方: libread book find <書名|ISBN> [--author <著者>] [--publisher <出版社>]"
	fs := flag.NewFlagSet("book find", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	author := fs.String("author", "", "著者で絞り込む")
	publisher := fs.String("publisher", "", "出版社で絞り込む")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErrorf("%s", usage)
	}
	query := strings.TrimSpace(strings.Join(pos, " "))
	if query == "" {
		return usageErrorf("%s", usage)
	}
	res, err := ndlClient(env).Search(ctx, ndl.Query{Text: query, Author: *author, Publisher: *publisher})
	if err != nil {
		return err
	}
	if len(res.Books) == 0 {
		fmt.Fprintln(env.Stdout, ndlAttribution)
		return usageErrorf("「%s」に一致する本は見つかりませんでした", query)
	}
	if res.Truncated {
		fmt.Fprintf(env.Stdout, "「%s」の検索結果（%d件中%d件を表示）\n", query, res.Total, len(res.Books))
	} else {
		fmt.Fprintf(env.Stdout, "「%s」の検索結果（%d件）\n", query, len(res.Books))
	}
	for i, b := range res.Books {
		fmt.Fprintf(env.Stdout, "%d. %s %s\n", i+1, b.ISBN, describeBook(b))
	}
	fmt.Fprintln(env.Stdout)
	if res.Truncated {
		fmt.Fprintf(env.Stdout, "%d件のうち上位%d件を表示しています。"+
			"見つからないときは --author <著者> や --publisher <出版社> で絞り込むか、ISBN で探してください\n", res.Total, len(res.Books))
	}
	fmt.Fprintln(env.Stdout, "登録するには: libread book add <ISBN> [--note <テキスト>]")
	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, ndlAttribution)
	return nil
}

// runBookAdd は ISBN の書誌を NDL サーチで引き、読みかけの本として登録する（FR-009, FR-010）。
func runBookAdd(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("book add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	note := fs.String("note", "", "読んだ位置などのメモ")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return usageErrorf("使い方: libread book add <ISBN> [--note <テキスト>]")
	}
	isbn, err := domain.ParseISBN(pos[0])
	if err != nil {
		return usageErrorf("%v。書名から探すときは libread book find <書名> を使ってください", err)
	}

	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	for _, b := range data.Books {
		if b.Book.ISBN == isbn {
			return usageErrorf("『%s』（ISBN %s）はすでに読みかけの本に登録されています", b.Book.Title, isbn)
		}
	}

	found, err := ndlClient(env).Search(ctx, ndl.Query{Text: isbn.String()})
	if err != nil {
		return err
	}
	var book domain.Book
	for _, b := range found.Books {
		if b.ISBN == isbn {
			book = b
			break
		}
	}
	if book.ISBN == "" {
		fmt.Fprintln(env.Stdout, ndlAttribution)
		return usageErrorf("ISBN %s の書誌が国立国会図書館サーチで見つかりませんでした", isbn)
	}

	data.Books = append(data.Books, domain.ReadingBook{Book: book, Note: *note, AddedOn: domain.DateOf(env.Now())})
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "読みかけの本に登録しました: %s（ISBN %s）\n", describeBook(book), isbn)
	if *note != "" {
		fmt.Fprintf(env.Stdout, "メモ: %s\n", *note)
	}
	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, ndlAttribution)
	return nil
}

// runBookList は読みかけの本とメモを登録順に表示する。
func runBookList(args []string, env Env) error {
	if len(args) != 0 {
		return usageErrorf("使い方: libread book list")
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	if len(data.Books) == 0 {
		fmt.Fprintln(env.Stdout, "読みかけの本は登録されていません。libread book find <書名> で本を探し、libread book add <ISBN> で登録してください")
		return nil
	}
	fmt.Fprintf(env.Stdout, "読みかけの本（%d冊）\n", len(data.Books))
	for i, b := range data.Books {
		fmt.Fprintf(env.Stdout, "%d. %s\n", i+1, describeBook(b.Book))
		fmt.Fprintf(env.Stdout, "   ISBN %s / 登録日 %s\n", b.Book.ISBN, b.AddedOn)
		if b.Note != "" {
			fmt.Fprintf(env.Stdout, "   メモ: %s\n", b.Note)
		}
	}
	return nil
}

// runBookNote はメモを上書きする。空文字なら消去する（FR-010）。
func runBookNote(args []string, env Env) error {
	if len(args) != 2 {
		return usageErrorf("使い方: libread book note <本> <テキスト>（空文字 \"\" でメモを消去）")
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	target, err := resolveBook(data.Books, args[0])
	if err != nil {
		return err
	}
	for i := range data.Books {
		if data.Books[i].Book.ISBN == target.Book.ISBN {
			data.Books[i].Note = args[1]
		}
	}
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	if args[1] == "" {
		fmt.Fprintf(env.Stdout, "メモを消去しました: 『%s』\n", target.Book.Title)
	} else {
		fmt.Fprintf(env.Stdout, "メモを更新しました: 『%s』\nメモ: %s\n", target.Book.Title, args[1])
	}
	return nil
}

// runBookRm は読みかけの本から外す。
func runBookRm(args []string, env Env) error {
	if len(args) != 1 {
		return usageErrorf("使い方: libread book rm <本>")
	}
	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	target, err := resolveBook(data.Books, args[0])
	if err != nil {
		return err
	}
	kept := data.Books[:0]
	for _, b := range data.Books {
		if b.Book.ISBN != target.Book.ISBN {
			kept = append(kept, b)
		}
	}
	data.Books = kept
	if err := store.Save(env.DataDir, data); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "読みかけの本から外しました: 『%s』\n", target.Book.Title)
	return nil
}

// describeBook は『書名』 著者 / 出版社 の形で本を表す。著者・出版社が空なら省く。
func describeBook(b domain.Book) string {
	s := "『" + b.Title + "』"
	var rest []string
	for _, v := range []string{b.Author, b.Publisher} {
		if v != "" {
			rest = append(rest, v)
		}
	}
	if len(rest) > 0 {
		s += " " + strings.Join(rest, " / ")
	}
	return s
}

// parseInterspersed はフラグと位置引数が混ざった args を解析し、位置引数を返す
// （flag パッケージは最初の位置引数で解析をやめるため）。
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
