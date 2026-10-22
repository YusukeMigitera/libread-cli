package calil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// appKeyPattern は値の入った appkey（URL のクエリ、または JSON のキー）に一致する。
var appKeyPattern = regexp.MustCompile(`appkey=[^&"'\s]+|"appkey"\s*:\s*"[^"]+"`)

// TestFixturesHaveNoAppKey は、記録済みレスポンスにアプリケーションキーが残っていないことを確かめる
// （Constitution 技術的制約: 秘密情報をリポジトリに含めない）。
func TestFixturesHaveNoAppKey(t *testing.T) {
	err := filepath.WalkDir("testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := appKeyPattern.Find(raw); m != nil {
			t.Errorf("%s にアプリケーションキーが含まれている: %s", path, m)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestAppKeyPattern(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "https://api.calil.jp/check?appkey=abc123&isbn=1", want: true},
		{in: `{"url": "https://api.calil.jp/check?appkey=abc123"}`, want: true},
		{in: `{"appkey": "abc123"}`, want: true},
		{in: "https://api.calil.jp/check?appkey=&isbn=1", want: false},
		{in: `{"url": "https://api.calil.jp/check?appkey="}`, want: false},
		{in: `{"appkey": ""}`, want: false},
		{in: `{"session": "S-0001"}`, want: false},
	}
	for _, tt := range tests {
		if got := appKeyPattern.MatchString(tt.in); got != tt.want {
			t.Errorf("appKeyPattern.MatchString(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
