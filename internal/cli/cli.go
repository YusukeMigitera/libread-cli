// Package cli は libread のサブコマンドを解析・実行し、結果をターミナルへ書く。
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"libread/internal/domain"
)

// 終了コード（contracts/cli.md「共通」）。
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Env はコマンドの実行に必要な外部依存。テストではすべて差し替える。
type Env struct {
	Stdout      io.Writer
	Stderr      io.Writer
	Now         func() time.Time
	Sleep       func(ctx context.Context, d time.Duration) error // カーリルのポーリング間隔の待ち。nil なら実際に待つ
	HTTPClient  *http.Client
	DataDir     string
	CacheDir    string
	CalilAppKey string
}

type command struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string, env Env) error
}

// commands は実装済みのサブコマンド。各ユーザーストーリーで追加する。
var commands = []command{checkCommand}

// Run は args（プログラム名を除く）を解析してサブコマンドを実行し、終了コードを返す。
func Run(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		args = []string{checkCommand.name} // 引数なしの libread は check（contracts/cli.md）
	}
	switch args[0] {
	case "help", "-h", "--help":
		writeUsage(env.Stdout)
		return ExitOK
	}
	for _, c := range commands {
		if c.name == args[0] {
			return exitCode(env, c.run(ctx, args[1:], env))
		}
	}
	fmt.Fprintf(env.Stderr, "libread: 不明なコマンドです: %s\n\n", args[0])
	writeUsage(env.Stderr)
	return ExitUsage
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "使い方: libread <コマンド> [引数]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "コマンド:")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-8s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, "  %-8s %s\n", "help", "この説明を表示する")
}

// usageError は使い方の誤り（引数不足・該当なし・候補が複数など）。終了コード 2 になる。
type usageError struct {
	msg string
}

func (e *usageError) Error() string { return e.msg }

func usageErrorf(format string, a ...any) error {
	return &usageError{msg: fmt.Sprintf(format, a...)}
}

func exitCode(env Env, err error) int {
	if err == nil {
		return ExitOK
	}
	fmt.Fprintf(env.Stderr, "libread: %v\n", err)
	var u *usageError
	if errors.As(err, &u) {
		return ExitUsage
	}
	return ExitError
}

// resolveBook は <本>（ISBN または書名の部分一致）を読みかけの本1冊に絞る。
func resolveBook(books []domain.ReadingBook, ref string) (domain.ReadingBook, error) {
	if isbn, err := domain.ParseISBN(ref); err == nil {
		for _, b := range books {
			if b.Book.ISBN == isbn {
				return b, nil
			}
		}
		return domain.ReadingBook{}, usageErrorf("ISBN %s の読みかけの本は登録されていません", isbn)
	}
	var matches []domain.ReadingBook
	for _, b := range books {
		if strings.Contains(b.Book.Title, ref) {
			matches = append(matches, b)
		}
	}
	switch len(matches) {
	case 0:
		return domain.ReadingBook{}, usageErrorf("「%s」に一致する読みかけの本はありません", ref)
	case 1:
		return matches[0], nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "「%s」に一致する読みかけの本が複数あります。ISBN で指定してください:", ref)
	for _, b := range matches {
		fmt.Fprintf(&sb, "\n  %s 『%s』", b.Book.ISBN, b.Book.Title)
	}
	return domain.ReadingBook{}, &usageError{msg: sb.String()}
}

// resolveLibrary は <図書館>（SystemID:LibKey または図書館名の部分一致）を利用図書館1館に絞る。
func resolveLibrary(libs []domain.RegisteredLibrary, ref string) (domain.RegisteredLibrary, error) {
	if id, err := domain.ParseLibraryID(ref); err == nil {
		for _, l := range libs {
			if l.ID == id {
				return l, nil
			}
		}
	}
	var matches []domain.RegisteredLibrary
	for _, l := range libs {
		if strings.Contains(l.Name, ref) {
			matches = append(matches, l)
		}
	}
	switch len(matches) {
	case 0:
		return domain.RegisteredLibrary{}, usageErrorf("「%s」に一致する利用図書館はありません", ref)
	case 1:
		return matches[0], nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "「%s」に一致する利用図書館が複数あります。SystemID:LibKey で指定してください:", ref)
	for _, l := range matches {
		fmt.Fprintf(&sb, "\n  %s %s", l.ID, l.Name)
	}
	return domain.RegisteredLibrary{}, &usageError{msg: sb.String()}
}
