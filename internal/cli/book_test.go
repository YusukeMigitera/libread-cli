package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"libread/internal/domain"
	"libread/internal/store"
)

// wantAttribution は NDL サーチの利用条件の出典表示。iss-ndl-opac のメタデータは CC BY なので、
// データプロバイダとデータ提供機関の名前、ライセンスへのリンクを含める（research.md R10）。
const wantAttribution = "書誌情報: 国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータを利用しています（https://ndlsearch.ndl.go.jp/）\n" +
	"ライセンス: クリエイティブ・コモンズ 表示 4.0 国際（https://creativecommons.org/licenses/by/4.0/legalcode.ja）\n"

// ndlFixture は internal/ndl/testdata の NDL サーチ OpenSearch の応答。
func ndlFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "ndl", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func loadData(t *testing.T, env Env) store.Data {
	t.Helper()
	d, err := store.Load(env.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// run は args を実行し、出力を毎回空にしてから終了コードを返す。
func run(env Env, stdout, stderr *bytes.Buffer, args ...string) int {
	stdout.Reset()
	stderr.Reset()
	return Run(context.Background(), args, env)
}

// US2 受け入れシナリオ 1〜3: 書名で探して登録し、メモを更新し、読み終えたら外す。
func TestBookWorkflow(t *testing.T) {
	svc := &fakeServices{ndl: ndlFixture(t, "opensearch_title.xml")}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "book", "find", "吾輩は猫である"); code != ExitOK {
		t.Fatalf("find: exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "book_find.golden", stdout.String())
	if !strings.Contains(stdout.String(), "345件中10件を表示") || !strings.Contains(stdout.String(), "--author") || strings.Contains(stdout.String(), "上位") {
		t.Errorf("一致件数と絞り込みの案内がない: %q", stdout.String())
	}
	if q := svc.recorded()[0].url.Query(); q.Get("title") != "吾輩は猫である" || q.Get("dpid") != "iss-ndl-opac" || q.Has("creator") {
		t.Errorf("find の要求 = %v", q)
	}

	svc.ndl = ndlFixture(t, "opensearch_isbn.xml")
	if code := run(env, stdout, stderr, "book", "add", "978-4-00-310101-8", "--note", "p.120 まで"); code != ExitOK {
		t.Fatalf("add: exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "book_add.golden", stdout.String())
	d := loadData(t, env)
	if len(d.Books) != 1 {
		t.Fatalf("登録された本 = %d, want 1", len(d.Books))
	}
	got := d.Books[0]
	if got.Book.ISBN != "9784003101018" || got.Book.Title != "吾輩は猫である" || got.Book.Author != "夏目, 漱石, 1867-1916" || got.Book.Publisher != "岩波書店" {
		t.Errorf("登録された書誌 = %+v", got.Book)
	}
	if got.Note != "p.120 まで" || got.AddedOn.String() != "2026-10-21" {
		t.Errorf("メモ = %q, 登録日 = %s", got.Note, got.AddedOn)
	}
	if q := svc.recorded()[1].url.Query(); q.Get("isbn") != "9784003101018" {
		t.Errorf("add の要求 = %v, want isbn=9784003101018", q)
	}

	if code := run(env, stdout, stderr, "book", "note", "吾輩", "p.250 まで"); code != ExitOK {
		t.Fatalf("note: exit code = %d, stderr = %s", code, stderr)
	}
	if n := loadData(t, env).Books[0].Note; n != "p.250 まで" {
		t.Errorf("更新後のメモ = %q", n)
	}

	if code := run(env, stdout, stderr, "book", "list"); code != ExitOK {
		t.Fatalf("list: exit code = %d, stderr = %s", code, stderr)
	}
	assertGolden(t, "book_list.golden", stdout.String())

	if code := run(env, stdout, stderr, "book", "rm", "猫"); code != ExitOK {
		t.Fatalf("rm: exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "吾輩は猫である") {
		t.Errorf("rm の出力 = %q", stdout.String())
	}
	if n := len(loadData(t, env).Books); n != 0 {
		t.Errorf("rm 後の本 = %d, want 0", n)
	}
	if _, err := resolveBook(loadData(t, env).Books, "猫"); !isUsage(err) {
		t.Errorf("外した本を <本> として選べる: err = %v", err)
	}

	for _, r := range svc.recorded() {
		if r.host != "ndlsearch.ndl.go.jp" {
			t.Errorf("NDL サーチ以外に要求した: %s", r.host)
		}
		if r.agent != userAgent {
			t.Errorf("User-Agent = %q", r.agent)
		}
	}
	if n := len(svc.recorded()); n != 2 {
		t.Errorf("要求 = %d, want 2（find と add で1回ずつ）", n)
	}
}

func TestBookAddNoteBeforeISBN(t *testing.T) {
	svc := &fakeServices{ndl: ndlFixture(t, "opensearch_isbn.xml")}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "book", "add", "--note", "第3章", "4003101014"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if b := loadData(t, env).Books; len(b) != 1 || b[0].Note != "第3章" || b[0].Book.ISBN != "9784003101018" {
		t.Errorf("登録された本 = %+v", b)
	}
}

func TestBookAddRejectsDuplicate(t *testing.T) {
	svc := &fakeServices{ndl: ndlFixture(t, "opensearch_isbn.xml")}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "book", "add", "9784003101018"); code != ExitOK {
		t.Fatalf("1回目: exit code = %d, stderr = %s", code, stderr)
	}
	// 10桁で指定しても同じ ISBN として扱う。
	if code := run(env, stdout, stderr, "book", "add", "4003101014"); code != ExitUsage {
		t.Errorf("2回目: exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "登録されています") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if n := len(loadData(t, env).Books); n != 1 {
		t.Errorf("本 = %d, want 1", n)
	}
	if n := len(svc.recorded()); n != 1 {
		t.Errorf("要求 = %d, want 1（重複は問い合わせる前に断る）", n)
	}
}

func TestBookAddErrors(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		ndl       string
		ndlStatus int
		wantCode  int
		wantInErr string
	}{
		{name: "引数なし", args: []string{"book", "add"}, wantCode: ExitUsage, wantInErr: "使い方"},
		{name: "ISBNが不正", args: []string{"book", "add", "9784003101019"}, wantCode: ExitUsage, wantInErr: "チェックディジット"},
		{name: "書名は受け付けない", args: []string{"book", "add", "吾輩は猫である"}, wantCode: ExitUsage, wantInErr: "book find"},
		{name: "書誌が見つからない", args: []string{"book", "add", "9784003101100"}, ndl: emptyRSS, wantCode: ExitUsage, wantInErr: "9784003101100"},
		{name: "NDLに接続できない", args: []string{"book", "add", "9784003101018"}, ndlStatus: http.StatusServiceUnavailable, wantCode: ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeServices{ndl: tt.ndl, ndlStatus: tt.ndlStatus}
			env, stdout, stderr := newCheckEnv(t, "", svc)
			if code := run(env, stdout, stderr, tt.args...); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr = %s)", code, tt.wantCode, stderr)
			}
			if !strings.Contains(stderr.String(), tt.wantInErr) {
				t.Errorf("stderr = %q, want %q を含む", stderr.String(), tt.wantInErr)
			}
			if n := len(loadData(t, env).Books); n != 0 {
				t.Errorf("本 = %d, want 0", n)
			}
		})
	}
}

const emptyRSS = `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`

// 著者・出版社で絞り込むと、書名が一致する本が先頭に来て、すべて表示できたときは件数の案内を出さない。
func TestBookFindNarrowed(t *testing.T) {
	for _, args := range [][]string{
		{"book", "find", "吾輩は猫である", "--author", "夏目漱石", "--publisher", "岩波書店"},
		{"book", "find", "--author", "夏目漱石", "--publisher=岩波書店", "吾輩は猫である"},
	} {
		svc := &fakeServices{ndl: ndlFixture(t, "opensearch_title_author_publisher.xml")}
		env, stdout, stderr := newCheckEnv(t, "", svc)
		if code := run(env, stdout, stderr, args...); code != ExitOK {
			t.Fatalf("%v: exit code = %d, stderr = %s", args, code, stderr)
		}
		assertGolden(t, "book_find_narrowed.golden", stdout.String())
		q := svc.recorded()[0].url.Query()
		if q.Get("title") != "吾輩は猫である" || q.Get("creator") != "夏目漱石" || q.Get("publisher") != "岩波書店" {
			t.Errorf("%v: 要求 = %v", args, q)
		}
	}
}

func TestBookFindNoResults(t *testing.T) {
	svc := &fakeServices{ndl: emptyRSS}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "book", "find", "存在しない本"); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "「存在しない本」に一致する本は見つかりませんでした") {
		t.Errorf("stderr = %q", stderr.String())
	}
	// 一致がないので、絞り込みの案内は出さない。
	if stdout.String() != wantAttribution {
		t.Errorf("stdout = %q, want 出典表示だけ", stdout.String())
	}
}

// 絞り込みで0件になったときは、指定した著者・出版社もメッセージに含める。
func TestBookFindNoResultsShowsFilters(t *testing.T) {
	svc := &fakeServices{ndl: emptyRSS}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "book", "find", "吾輩は猫である", "--author", "夏目 漱石", "--publisher", "岩波書店"); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "「吾輩は猫である」（著者: 夏目 漱石、出版社: 岩波書店）に一致する本は見つかりませんでした") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// 一致はあるのに、取得した記録に ISBN のある本が1件もないときも、絞り込みの案内を出す。
func TestBookFindNoISBNInFetchedRecords(t *testing.T) {
	const rss = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<rss xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:openSearch="http://a9.com/-/spec/opensearchrss/1.0/" version="2.0"><channel>` +
		`<openSearch:totalResults>345</openSearch:totalResults>` +
		`<item><dc:title>内田百間全集</dc:title></item>` +
		`<item><dc:title>漱石全集</dc:title></item>` +
		`</channel></rss>`
	svc := &fakeServices{ndl: rss}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "book", "find", "吾輩は猫である"); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	assertGolden(t, "book_find_no_isbn.golden", stdout.String())
	if !strings.Contains(stderr.String(), "吾輩は猫である") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestBookFindUsage(t *testing.T) {
	svc := &fakeServices{}
	env, stdout, stderr := newCheckEnv(t, "", svc)
	for _, args := range [][]string{
		{"book", "find"},
		{"book", "find", "--author", "夏目漱石"},
		{"book", "find", "吾輩は猫である", "--no-such-flag"},
	} {
		if code := run(env, stdout, stderr, args...); code != ExitUsage {
			t.Errorf("%v: exit code = %d, want %d", args, code, ExitUsage)
		}
	}
	if n := len(svc.recorded()); n != 0 {
		t.Errorf("要求 = %d, want 0", n)
	}
}

func TestBookNote(t *testing.T) {
	env, stdout, stderr := newCheckEnv(t, "check_example.json", &fakeServices{})

	if code := run(env, stdout, stderr, "book", "note", "9784003101100", "p.42"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	b, _ := resolveBook(loadData(t, env).Books, "9784003101100")
	if b.Note != "p.42" {
		t.Errorf("メモ = %q, want p.42", b.Note)
	}

	// 空文字で消去する。
	if code := run(env, stdout, stderr, "book", "note", "9784003101100", ""); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	b, _ = resolveBook(loadData(t, env).Books, "9784003101100")
	if b.Note != "" {
		t.Errorf("メモ = %q, want 空", b.Note)
	}
	if !strings.Contains(stdout.String(), "消去") {
		t.Errorf("stdout = %q", stdout.String())
	}

	for _, args := range [][]string{
		{"book", "note", "9784003101100"},
		{"book", "note", "9784003101100", "a", "b"},
		{"book", "note", "三四郎", "p.1"},
	} {
		if code := run(env, stdout, stderr, args...); code != ExitUsage {
			t.Errorf("%v: exit code = %d, want %d", args, code, ExitUsage)
		}
	}
}

func TestBookRm(t *testing.T) {
	env, stdout, stderr := newCheckEnv(t, "check_example.json", &fakeServices{})
	before := loadData(t, env)

	// 複数一致は候補を示して何も消さない。
	if code := run(env, stdout, stderr, "book", "rm", "タイトル"); code != ExitUsage {
		t.Errorf("複数一致: exit code = %d, want %d", code, ExitUsage)
	}
	if code := run(env, stdout, stderr, "book", "rm", "タイトルP"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	after := loadData(t, env)
	if len(after.Books) != len(before.Books)-1 {
		t.Fatalf("本 = %d, want %d", len(after.Books), len(before.Books)-1)
	}
	for _, b := range after.Books {
		if strings.Contains(b.Book.Title, "タイトルP") {
			t.Errorf("外したはずの本が残っている: %+v", b)
		}
	}
	if len(after.Libraries) != len(before.Libraries) {
		t.Errorf("利用図書館が変わった: %d → %d", len(before.Libraries), len(after.Libraries))
	}
}

func TestBookListEmpty(t *testing.T) {
	env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
	if code := run(env, stdout, stderr, "book", "list"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "libread book find") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestBookUsage(t *testing.T) {
	for _, args := range [][]string{{"book"}, {"book", "no-such"}} {
		env, stdout, stderr := newCheckEnv(t, "", &fakeServices{})
		if code := run(env, stdout, stderr, args...); code != ExitUsage {
			t.Errorf("%v: exit code = %d, want %d", args, code, ExitUsage)
		}
		if !strings.Contains(stderr.String(), "book find") {
			t.Errorf("%v: stderr = %q", args, stderr.String())
		}
	}
}

// メモ（読んだ位置）はどの外部サービスにも送らない（Constitution 技術的制約）。
func TestBookNoteIsNeverSent(t *testing.T) {
	const note = "秘密のメモ p.120"
	svc := &fakeServices{ndl: ndlFixture(t, "opensearch_isbn.xml")}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	if code := run(env, stdout, stderr, "book", "add", "9784003101018", "--note", note); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if code := run(env, stdout, stderr, "book", "find", "吾輩は猫である"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	for _, r := range svc.recorded() {
		raw, err := url.QueryUnescape(r.url.String())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "秘密") {
			t.Errorf("メモが外部に送られた: %s", raw)
		}
	}
}

// book find と book add の出力、libread help は NDL サーチの出典表示で終わる（research.md R10）。
func TestNDLAttribution(t *testing.T) {
	svc := &fakeServices{ndl: ndlFixture(t, "opensearch_isbn.xml")}
	env, stdout, stderr := newCheckEnv(t, "", svc)

	for _, args := range [][]string{
		{"book", "find", "9784003101018"},
		{"book", "add", "9784003101018"},
		{"help"},
	} {
		if code := run(env, stdout, stderr, args...); code != ExitOK {
			t.Fatalf("%v: exit code = %d, stderr = %s", args, code, stderr)
		}
		if !strings.HasSuffix(stdout.String(), wantAttribution) {
			t.Errorf("%v: stdout = %q, want 出典表示で終わる", args, stdout.String())
		}
	}
}

// 10桁・ハイフン付きの ISBN で探しても、候補は13桁の ISBN で表示する。
func TestBookFindShowsNormalizedISBN(t *testing.T) {
	isbn, err := domain.ParseISBN("4-00-310101-4")
	if err != nil {
		t.Fatal(err)
	}
	svc := &fakeServices{ndl: ndlFixture(t, "opensearch_isbn.xml")}
	env, stdout, stderr := newCheckEnv(t, "", svc)
	if code := run(env, stdout, stderr, "book", "find", "4-00-310101-4"); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), isbn.String()) {
		t.Errorf("stdout = %q, want %s を含む", stdout.String(), isbn)
	}
}

// NDL サーチは応答が遅いことがあるので、NDL への要求だけタイムアウトを30秒にする（research.md R3）。
// 他のサービスに使う env.HTTPClient は変えず、Transport はそのまま使う。
func TestNDLClientTimeout(t *testing.T) {
	transport := &http.Transport{}
	env := Env{HTTPClient: &http.Client{Timeout: 10 * time.Second, Transport: transport}}

	c := ndlClient(env)
	if c.HTTP.Timeout != 30*time.Second {
		t.Errorf("NDL のタイムアウト = %v, want 30s", c.HTTP.Timeout)
	}
	if c.HTTP.Transport != transport {
		t.Error("NDL のクライアントが env.HTTPClient の Transport を使っていない")
	}
	if env.HTTPClient.Timeout != 10*time.Second {
		t.Errorf("env.HTTPClient のタイムアウトが変わった: %v", env.HTTPClient.Timeout)
	}
}
