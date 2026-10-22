package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"libread/internal/domain"
)

func testEnv(t *testing.T) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return Env{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Now:      func() time.Time { return time.Date(2026, 10, 21, 14, 5, 0, 0, domain.Tokyo()) },
		DataDir:  t.TempDir(),
		CacheDir: t.TempDir(),
	}, &stdout, &stderr
}

func TestRunUnknownCommand(t *testing.T) {
	env, _, stderr := testEnv(t)
	if code := Run(context.Background(), []string{"no-such-command"}, env); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "no-such-command") || !strings.Contains(stderr.String(), "使い方") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		env, stdout, _ := testEnv(t)
		if code := Run(context.Background(), []string{arg}, env); code != ExitOK {
			t.Errorf("%s: exit code = %d, want %d", arg, code, ExitOK)
		}
		if !strings.Contains(stdout.String(), "使い方") {
			t.Errorf("%s: stdout = %q", arg, stdout.String())
		}
	}
}

func TestRunMapsErrorsToExitCodes(t *testing.T) {
	env, _, stderr := testEnv(t)
	if code := exitCode(env, nil); code != ExitOK {
		t.Errorf("nil: %d", code)
	}
	if code := exitCode(env, usageErrorf("引数が足りません")); code != ExitUsage {
		t.Errorf("usage: %d", code)
	}
	if code := exitCode(env, errors.New("読み込み失敗")); code != ExitError {
		t.Errorf("runtime: %d", code)
	}
	if !strings.Contains(stderr.String(), "引数が足りません") || !strings.Contains(stderr.String(), "読み込み失敗") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func readingBook(t *testing.T, isbn, title string) domain.ReadingBook {
	t.Helper()
	i, err := domain.ParseISBN(isbn)
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.NewBook(i, title, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return domain.ReadingBook{Book: b}
}

func isUsage(err error) bool {
	var u *usageError
	return errors.As(err, &u)
}

func TestResolveBook(t *testing.T) {
	books := []domain.ReadingBook{
		readingBook(t, "9784003101018", "吾輩は猫である"),
		readingBook(t, "9784806120575", "猫の事務所"),
		readingBook(t, "9784003101100", "坊っちゃん"),
	}

	tests := []struct {
		name      string
		ref       string
		wantISBN  domain.ISBN
		wantErr   bool
		wantInErr []string
	}{
		{name: "ISBN13", ref: "9784003101018", wantISBN: "9784003101018"},
		{name: "ISBNハイフン付き", ref: "978-4-00-310101-8", wantISBN: "9784003101018"},
		{name: "ISBN10", ref: "4003101014", wantISBN: "9784003101018"},
		{name: "書名の部分一致", ref: "坊っ", wantISBN: "9784003101100"},
		{name: "未登録のISBN", ref: "9784806120506", wantErr: true},
		{name: "該当なし", ref: "三四郎", wantErr: true, wantInErr: []string{"三四郎"}},
		{name: "複数一致は候補を表示", ref: "猫", wantErr: true, wantInErr: []string{"吾輩は猫である", "猫の事務所", "9784003101018", "9784806120575"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveBook(books, tt.ref)
			if tt.wantErr {
				if err == nil || !isUsage(err) {
					t.Fatalf("err = %v, want usage error", err)
				}
				for _, s := range tt.wantInErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q に %q が含まれない", err.Error(), s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.Book.ISBN != tt.wantISBN {
				t.Errorf("got %s, want %s", got.Book.ISBN, tt.wantISBN)
			}
		})
	}
}

func TestResolveLibrary(t *testing.T) {
	libs := []domain.RegisteredLibrary{
		{ID: domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "中央"}, Name: "世田谷区立中央図書館"},
		{ID: domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "経堂"}, Name: "世田谷区立経堂図書館"},
		{ID: domain.LibraryID{SystemID: "Tokyo_Meguro", LibKey: "中央"}, Name: "目黒区立八雲中央図書館"},
	}

	tests := []struct {
		name      string
		ref       string
		want      domain.LibraryID
		wantErr   bool
		wantInErr []string
	}{
		{name: "ID指定", ref: "Tokyo_Meguro:中央", want: libs[2].ID},
		{name: "名前の部分一致", ref: "経堂", want: libs[1].ID},
		{name: "未登録のID", ref: "Tokyo_Setagaya:梅丘", wantErr: true},
		{name: "該当なし", ref: "渋谷", wantErr: true, wantInErr: []string{"渋谷"}},
		{name: "複数一致は候補を表示", ref: "中央", wantErr: true, wantInErr: []string{"Tokyo_Setagaya:中央", "Tokyo_Meguro:中央", "世田谷区立中央図書館", "目黒区立八雲中央図書館"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveLibrary(libs, tt.ref)
			if tt.wantErr {
				if err == nil || !isUsage(err) {
					t.Fatalf("err = %v, want usage error", err)
				}
				for _, s := range tt.wantInErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q に %q が含まれない", err.Error(), s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.ID != tt.want {
				t.Errorf("got %v, want %v", got.ID, tt.want)
			}
		})
	}
}
