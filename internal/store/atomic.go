package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic は同じディレクトリの一時ファイルに書いてから rename する。途中で落ちても既存ファイルは壊れない。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ディレクトリ %s を作成できません: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("一時ファイルを作成できません: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename に成功した後は何もしない

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("%s に書き込めません: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("%s を書き出せません: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s を閉じられません: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("%s を保存できません: %w", path, err)
	}
	return nil
}
