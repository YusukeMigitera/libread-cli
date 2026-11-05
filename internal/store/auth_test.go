package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAuthStateMissingFileIsGeneration1(t *testing.T) {
	got, err := LoadAuthState(t.TempDir())
	if err != nil {
		t.Fatalf("LoadAuthState: %v", err)
	}
	if got.Generation != 1 {
		t.Errorf("Generation = %d, want 1", got.Generation)
	}
}

func TestAuthStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveAuthState(dir, AuthState{Generation: 7}); err != nil {
		t.Fatalf("SaveAuthState: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"version\":1,\"generation\":7}\n"; string(raw) != want {
		t.Errorf("auth.json = %q, want %q", raw, want)
	}
	got, err := LoadAuthState(dir)
	if err != nil {
		t.Fatalf("LoadAuthState: %v", err)
	}
	if got.Generation != 7 {
		t.Errorf("Generation = %d, want 7", got.Generation)
	}
}

func TestLoadAuthStateRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"generation0":  `{"version": 1, "generation": 0}`,
		"generation負":  `{"version": 1, "generation": -1}`,
		"generationなし": `{"version": 1}`,
		"未知のversion":   `{"version": 2, "generation": 1}`,
		"壊れたJSON":      `{"version": 1,`,
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadAuthState(dir); err == nil {
				t.Error("want error")
			}
		})
	}
}

func TestSaveAuthStateRejectsInvalidGeneration(t *testing.T) {
	if err := SaveAuthState(t.TempDir(), AuthState{Generation: 0}); err == nil {
		t.Error("want error")
	}
}
