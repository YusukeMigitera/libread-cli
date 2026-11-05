package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	authFileName      = "auth.json"
	authSchemaVersion = 1
)

// AuthState は本人確認の状態（data-model.md AuthState）。利用者データとは別の auth.json に保存する。
// Generation はセッション世代番号で、1以上。型は internal/auth の SessionGeneration に変換して使う
// （store から auth への依存を作らないため、ここでは検証済みの int で持つ）。
type AuthState struct {
	Generation int
}

type authFile struct {
	Version    int `json:"version"`
	Generation int `json:"generation"`
}

// LoadAuthState は dir/auth.json を読む。ファイルがなければ Generation 1 を返す。
func LoadAuthState(dir string) (AuthState, error) {
	path := filepath.Join(dir, authFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return AuthState{Generation: 1}, nil
	}
	if err != nil {
		return AuthState{}, fmt.Errorf("%s を読み込めません: %w", path, err)
	}
	var f authFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return AuthState{}, fmt.Errorf("%s の形式が正しくありません: %w", path, err)
	}
	if f.Version != authSchemaVersion {
		return AuthState{}, fmt.Errorf("%s: 対応していないバージョン %d です", path, f.Version)
	}
	if f.Generation < 1 {
		return AuthState{}, fmt.Errorf("%s: セッション世代番号 %d は1以上にしてください", path, f.Generation)
	}
	return AuthState{Generation: f.Generation}, nil
}

// SaveAuthState は s を dir/auth.json にアトミックに書き込む。
func SaveAuthState(dir string, s AuthState) error {
	if s.Generation < 1 {
		return fmt.Errorf("セッション世代番号 %d は1以上にしてください", s.Generation)
	}
	raw, err := json.Marshal(authFile{Version: authSchemaVersion, Generation: s.Generation})
	if err != nil {
		return fmt.Errorf("本人確認の状態を変換できません: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, authFileName), append(raw, '\n'))
}
