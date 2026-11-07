package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// SC-103: Web の検索結果は、001 の CLI と同じ館・同じ順・同じ理由・同じ出典になる。
// testdata の *.golden は 001 の CLI（internal/cli/testdata）の出力をそのまま写したもの。CLI を廃止しても残す。
func TestCheckMatchesCLIScenarios(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		golden string
		target string
		svc    func() *fakeServices
		cache  string
	}{
		{
			// TestCheckContractExample: 開館中・開館前・閉館まで30分未満・臨時休館・開館情報未入力・予約中・所蔵なし。
			name: "契約の例", data: "check_example.json", golden: "check_example.golden", target: "/api/check",
			svc: func() *fakeServices { return &fakeServices{calil: []string{exampleRunning, exampleFinal}} },
		},
		{
			// TestCheckScenario1SingleBook: 本を指定して検索する（US1 受け入れシナリオ 1）。
			name: "本を指定", data: "check_scenario1.json", golden: "check_scenario1.golden", target: "/api/check?isbn=9784003101018",
			svc: func() *fakeServices {
				return &fakeServices{calil: []string{`{"session": "S-1", "continue": 0, "books": {
				  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可", "梅丘図書館": "貸出可", "経堂図書館": "貸出中"}}}}}`}}
			},
		},
		{
			// TestCheckScenario5NoneReadable: どの館でも読めない（US1 受け入れシナリオ 5）。
			name: "どこでも読めない", data: "check_scenario1.json", golden: "check_none.golden", target: "/api/check",
			svc: func() *fakeServices {
				return &fakeServices{calil: []string{`{"session": "S-1", "continue": 0, "books": {
				  "9784003101018": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出中", "梅丘図書館": "貸出可", "経堂図書館": "予約中"}}},
				  "9784003101100": {"Tokyo_Setagaya": {"status": "OK", "libkey": {"経堂図書館": "貸出中"}}}}}`}}
			},
		},
		{
			// TestCheckEdgeCases: 本日終了・祝日判定不可・取得タイムアウト・取得失敗。
			name: "境界", data: "check_edge.json", golden: "check_edge.golden", target: "/api/check",
			svc: func() *fakeServices {
				return &fakeServices{calil: []string{`{"session": "S-1", "continue": 1, "books": {
				  "9784003101018": {
				    "Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可", "経堂図書館": "貸出可"}},
				    "Tokyo_Meguro": {"status": "Running", "libkey": {}},
				    "Kanagawa_Kawasaki": {"status": "Error", "libkey": {}}}}}`}, holidaysStatus: http.StatusInternalServerError}
			},
		},
		{
			// TestCheckStaleCacheAndFailedRefetch: 取得し直せず、古いキャッシュだけがある。
			name: "古い情報", data: "check_single.json", golden: "check_stale.golden", target: "/api/check",
			svc:   func() *fakeServices { return &fakeServices{calilStatus: http.StatusServiceUnavailable} },
			cache: `"9784003101018|Tokyo_Setagaya": {"isbn": "9784003101018", "systemId": "Tokyo_Setagaya", "systemStatus": "OK", "byLibKey": {"中央図書館": "貸出可"}, "fetchedAt": "2026-10-21T13:00:00+09:00"}`,
		},
		{
			// TestCheckFallsBackToCachePerSystem: 図書館システムごとに取得時刻が違う。
			name: "取得時刻が本ごと", data: "check_single.json", golden: "check_mixed_times.golden", target: "/api/check",
			svc: func() *fakeServices {
				return &fakeServices{calil: []string{`{"session": "S-1", "continue": 0, "books": {
				  "9784003101018": {
				    "Tokyo_Setagaya": {"status": "OK", "libkey": {"中央図書館": "貸出可"}},
				    "Tokyo_Meguro": {"status": "Error", "libkey": {}}}}}`}}
			},
			cache: `"9784003101018|Tokyo_Meguro": {"isbn": "9784003101018", "systemId": "Tokyo_Meguro", "systemStatus": "OK", "byLibKey": {"八雲中央図書館": "館内のみ"}, "fetchedAt": "2026-10-21T13:50:00+09:00"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newCheckServer(t, tt.svc(), checkStart)
			s.writeDataFile(t, tt.data)
			if tt.cache != "" {
				s.writeHoldingsCache(t, tt.cache)
			}
			rec := s.get(t, tt.target)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			var body checkJSONBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			got := viewOfResponse(body)
			want := parseCheckGolden(t, tt.golden)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("CLI（%s）と違う\n--- got ---\n%+v\n--- want ---\n%+v", tt.golden, got, want)
			}
		})
	}
}

// checkView は、CLI の出力と API の応答の両方から取り出す、比べる部分。
type checkView struct {
	Readable        []libraryLine
	NotReadable     []libraryLine
	HoldingsLabel   string
	Links           []linkLine
	OpeningInfo     string
	OpeningInfoNote string
}

type libraryLine struct {
	Name, Label string
	Books       []bookLine
}

type bookLine struct{ Title, Label string }

type linkLine struct{ Title, URL string }

func viewOfResponse(b checkJSONBody) checkView {
	libs := func(in []checkJSONLibrary) []libraryLine {
		var out []libraryLine
		for _, l := range in {
			line := libraryLine{Name: l.Library.Name, Label: l.Opening.Label}
			for _, bk := range l.Books {
				line.Books = append(line.Books, bookLine{Title: bk.Title, Label: bk.Label})
			}
			out = append(out, line)
		}
		return out
	}
	v := checkView{
		Readable:        libs(b.Readable),
		NotReadable:     libs(b.NotReadable),
		HoldingsLabel:   b.Sources.Holdings.Label,
		OpeningInfo:     b.Sources.OpeningInfo.Label,
		OpeningInfoNote: b.Sources.OpeningInfo.Note,
	}
	for _, l := range b.Sources.Holdings.Links {
		v.Links = append(v.Links, linkLine{Title: l.Title, URL: l.URL})
	}
	return v
}

// parseCheckGolden は CLI の check の出力（001 contracts/cli.md）を読む。
// 館は「N. <館名>  <label>」（読める館）と「- <館名>  <label>」（読めない館）、本は「   - 『<書名>』 <label>」。
func parseCheckGolden(t *testing.T, name string) checkView {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var v checkView
	var section *[]libraryLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "■ 今日読める図書館":
			section = &v.Readable
		case line == "■ 今日は読めない図書館":
			section = &v.NotReadable
		case line == "今日読める図書館はありません" || line == "":
		case strings.HasPrefix(line, "所蔵情報: "):
			v.HoldingsLabel = line
			section = nil
		case strings.HasPrefix(line, "  『"):
			title, url, ok := strings.Cut(strings.TrimPrefix(line, "  『"), "』 ")
			if !ok {
				t.Fatalf("%s: 出典のリンクの行を読めない: %q", name, line)
			}
			v.Links = append(v.Links, linkLine{Title: title, URL: url})
		case strings.HasPrefix(line, "開館情報: "):
			v.OpeningInfo = line
		case strings.HasPrefix(line, "※ "):
			v.OpeningInfoNote = line
		case strings.HasPrefix(line, "   - 『"):
			if section == nil || len(*section) == 0 {
				t.Fatalf("%s: 館のない本の行: %q", name, line)
			}
			title, label, ok := strings.Cut(strings.TrimPrefix(line, "   - 『"), "』 ")
			if !ok {
				t.Fatalf("%s: 本の行を読めない: %q", name, line)
			}
			last := &(*section)[len(*section)-1]
			last.Books = append(last.Books, bookLine{Title: title, Label: label})
		case section != nil:
			rest := line
			if section == &v.Readable {
				_, after, ok := strings.Cut(line, ". ")
				if !ok {
					t.Fatalf("%s: 読める館の行を読めない: %q", name, line)
				}
				rest = after
			} else {
				rest = strings.TrimPrefix(line, "- ")
			}
			libName, label, ok := strings.Cut(rest, "  ")
			if !ok {
				t.Fatalf("%s: 館の行を読めない: %q", name, line)
			}
			*section = append(*section, libraryLine{Name: libName, Label: label})
		default:
			// 1行目の基準時刻。checkedAt はほかのテストで確かめる。
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return v
}

// parseCheckGolden 自体が CLI の出力を正しく読めることを確かめる。
func TestParseCheckGolden(t *testing.T) {
	v := parseCheckGolden(t, "check_example.golden")
	if len(v.Readable) != 3 || len(v.NotReadable) != 3 {
		t.Fatalf("readable = %d, notReadable = %d, want 3・3", len(v.Readable), len(v.NotReadable))
	}
	if got := v.Readable[2]; got.Name != "世田谷区立経堂図書館" || got.Label != "開館中 〜14:30（閉館まで30分未満）" || len(got.Books) != 2 {
		t.Errorf("readable[2] = %+v", got)
	}
	if got := v.NotReadable[1]; got.Name != "世田谷区立烏山図書館" || got.Books[0] != (bookLine{"本のタイトルP", "不明（蔵書あり／貸出状況不明）"}) {
		t.Errorf("notReadable[1] = %+v", got)
	}
	if len(v.NotReadable[0].Books) != 0 || len(v.Links) != 2 || v.OpeningInfoNote == "" {
		t.Errorf("view = %+v", v)
	}
}
