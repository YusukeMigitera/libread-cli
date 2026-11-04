package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type cachedValue struct {
	Name  string            `json:"name"`
	Items map[string]string `json:"items"`
}

func TestCachePutGet(t *testing.T) {
	c := NewCache(t.TempDir())
	want := cachedValue{Name: "x", Items: map[string]string{"中央": "貸出可"}}
	storedAt := time.Date(2026, 10, 21, 14, 5, 0, 0, time.UTC)

	if err := Put(c, "holdings.json", want, storedAt); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, at, ok, err := Get[cachedValue](c, "holdings.json")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if got.Name != want.Name || got.Items["中央"] != "貸出可" {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !at.Equal(storedAt) {
		t.Errorf("storedAt = %v, want %v", at, storedAt)
	}
}

func TestCacheMiss(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "not-yet-created"))
	_, _, ok, err := Get[cachedValue](c, "holdings.json")
	if err != nil || ok {
		t.Errorf("ok=%v err=%v, want miss without error", ok, err)
	}
}

func TestCacheCorruptFileIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "holdings.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Get[cachedValue](NewCache(dir), "holdings.json"); err == nil {
		t.Error("want error")
	}
}

func TestCacheRejectsPathInName(t *testing.T) {
	c := NewCache(t.TempDir())
	for _, name := range []string{"../x.json", "a/b.json", "", "."} {
		if err := Put(c, name, cachedValue{}, time.Now()); err == nil {
			t.Errorf("Put(%q) want error", name)
		}
	}
}

func TestCachePutLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	for range 2 {
		if err := Put(c, "holidays-2026.json", cachedValue{Name: "y"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d, want 1", len(entries))
	}
}

func TestResolveCacheDir(t *testing.T) {
	got, err := ResolveCacheDir("/tmp/custom-cache")
	if err != nil || got != "/tmp/custom-cache" {
		t.Errorf("override: got %q, %v", got, err)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		t.Skip("UserCacheDir を取得できない環境")
	}
	got, err = ResolveCacheDir("")
	if err != nil || got != filepath.Join(base, "libread") {
		t.Errorf("default: got %q, %v", got, err)
	}
}
