package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cache はキャッシュファイルを置くディレクトリ。
type Cache struct {
	dir string
}

func NewCache(dir string) Cache { return Cache{dir: dir} }

type cacheEntry[T any] struct {
	StoredAt time.Time `json:"storedAt"`
	Value    T         `json:"value"`
}

// Get は name のキャッシュを読む。ファイルがなければ ok=false。
func Get[T any](c Cache, name string) (value T, storedAt time.Time, ok bool, err error) {
	path, err := c.path(name)
	if err != nil {
		return value, time.Time{}, false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return value, time.Time{}, false, nil
	}
	if err != nil {
		return value, time.Time{}, false, fmt.Errorf("キャッシュ %s を読み込めません: %w", path, err)
	}
	var e cacheEntry[T]
	if err := json.Unmarshal(raw, &e); err != nil {
		return value, time.Time{}, false, fmt.Errorf("キャッシュ %s の形式が正しくありません: %w", path, err)
	}
	return e.Value, e.StoredAt, true, nil
}

// Put は value を storedAt とともに name へアトミックに書き込む。
func Put[T any](c Cache, name string, value T, storedAt time.Time) error {
	path, err := c.path(name)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(cacheEntry[T]{StoredAt: storedAt, Value: value})
	if err != nil {
		return fmt.Errorf("キャッシュ %s を変換できません: %w", name, err)
	}
	return writeFileAtomic(path, raw)
}

func (c Cache) path(name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("キャッシュ名 %q にはディレクトリを含められません", name)
	}
	return filepath.Join(c.dir, name), nil
}
