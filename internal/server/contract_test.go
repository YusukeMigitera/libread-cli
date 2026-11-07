package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// examplesDir は API の応答の例（contracts/http-api.md「共通」）。画面のテストも同じファイルを使う（Constitution 原則 III）。
var examplesDir = filepath.Join("..", "..", "specs", "002-web-mobile-access", "contracts", "examples")

// assertMatchesExample は、body のすべての階層のキーの集合が examples/<name>.json と同じであることを確かめる。値は比べない。
// 配列は、同じ位置の例の要素（なければ最後の要素）と比べる。例の配列が空なら中身は比べない。
func assertMatchesExample(t *testing.T, name string, body []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(examplesDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("examples/%s.json: %v", name, err)
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("応答が JSON でない: %v\n%s", err, body)
	}
	for _, d := range shapeDiff("$", want, got) {
		t.Errorf("examples/%s.json と形が違う: %s\n%s", name, d, body)
	}
}

func shapeDiff(path string, want, got any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{path + " がオブジェクトでない"}
		}
		var diffs []string
		for _, k := range sortedKeys(w) {
			if _, ok := g[k]; !ok {
				diffs = append(diffs, path+"."+k+" がない")
			}
		}
		for _, k := range sortedKeys(g) {
			wv, ok := w[k]
			if !ok {
				diffs = append(diffs, path+"."+k+" は例にない")
				continue
			}
			diffs = append(diffs, shapeDiff(path+"."+k, wv, g[k])...)
		}
		return diffs
	case []any:
		g, ok := got.([]any)
		if !ok {
			return []string{path + " が配列でない"}
		}
		if len(w) == 0 {
			return nil
		}
		var diffs []string
		for i, gv := range g {
			diffs = append(diffs, shapeDiff(path+"["+strconv.Itoa(i)+"]", w[min(i, len(w)-1)], gv)...)
		}
		return diffs
	default:
		// 値（null を含む）は比べない。null の位置にオブジェクトが来てもよい（例: 休館の曜日と開館の曜日）。
		return nil
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestShapeDiff(t *testing.T) {
	want := map[string]any{"a": 1.0, "b": map[string]any{"c": "x"}, "list": []any{map[string]any{"k": 1.0}}}
	ok := map[string]any{"a": 2.0, "b": map[string]any{"c": "y"}, "list": []any{map[string]any{"k": 3.0}, map[string]any{"k": 4.0}}}
	if d := shapeDiff("$", want, ok); len(d) != 0 {
		t.Errorf("同じ形なのに差がある: %v", d)
	}
	bad := map[string]any{"a": 2.0, "b": map[string]any{"c": "y", "extra": 1.0}, "list": []any{map[string]any{}}}
	if d := shapeDiff("$", want, bad); len(d) != 2 {
		t.Errorf("差 = %v, want 2件（b.extra と list[0].k）", d)
	}
}

func TestExamplesAreValidJSON(t *testing.T) {
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(examplesDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(raw) {
			t.Errorf("%s が JSON でない", e.Name())
		}
	}
}
