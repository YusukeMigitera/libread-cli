// Package ndl は国立国会図書館サーチ OpenSearch API のクライアント（research.md R3）。
package ndl

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"libread/internal/domain"
)

// DefaultBaseURL は NDL サーチ OpenSearch API の URL。
const DefaultBaseURL = "https://ndlsearch.ndl.go.jp/api/opensearch"

const (
	// maxResults は1回の検索で返す候補の上限（contracts/cli.md の book find）。
	maxResults = 10
	// fetchCount は1回の要求で取得する件数。NDL サーチは書名の読みの五十音順で返すので、
	// 多めに取得して書名の一致度で並べ替える。増やすほど応答が遅くなる（research.md R3）。
	fetchCount = 20
	// dataProvider は検索対象を国立国会図書館の蔵書に限る（research.md R3・R10）。
	dataProvider = "iss-ndl-opac"
)

// Client は NDL サーチ OpenSearch API のクライアント。
type Client struct {
	BaseURL   string // 空なら DefaultBaseURL
	HTTP      *http.Client
	UserAgent string
}

// Query は書誌の検索条件。Author・Publisher は空なら指定しない。
type Query struct {
	Text      string // 書名、または ISBN
	Author    string
	Publisher string
}

// Result は検索結果。Total は NDL サーチで一致した件数（ISBN のない記録も含む）。
// Truncated は、一致した本の一部しか Books に入っていないことを表す。
type Result struct {
	Books     []domain.Book
	Total     int
	Truncated bool
}

// Search は q.Text を書名として（ISBN として読めるなら ISBN として）検索し、最大10件の書誌を返す。
// ISBN や書名のない記録は除き、同じ ISBN の記録は最初の1件だけを返す。
// 書名で探すときは、書名が検索語と完全一致 → 検索語で始まる → その他の順に並べる（同じ順位の中は NDL の順）。
func (c *Client) Search(ctx context.Context, q Query) (Result, error) {
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return Result{}, errors.New("検索語が空です")
	}
	params := url.Values{
		"cnt":  {strconv.Itoa(fetchCount)},
		"dpid": {dataProvider},
	}
	isbn, err := domain.ParseISBN(text)
	byISBN := err == nil
	if byISBN {
		params.Set("isbn", isbn.String())
	} else {
		params.Set("title", text)
	}
	if a := strings.TrimSpace(q.Author); a != "" {
		params.Set("creator", a)
	}
	if p := strings.TrimSpace(q.Publisher); p != "" {
		params.Set("publisher", p)
	}
	resp, err := c.get(ctx, params)
	if err != nil {
		return Result{}, err
	}

	var books []domain.Book
	seen := map[domain.ISBN]bool{}
	for _, it := range resp.Channel.Items {
		b, ok := it.toDomain()
		if !ok || seen[b.ISBN] {
			continue
		}
		seen[b.ISBN] = true
		books = append(books, b)
	}
	if !byISBN {
		slices.SortStableFunc(books, func(a, b domain.Book) int { return titleRank(a.Title, text) - titleRank(b.Title, text) })
	}
	// totalResults のない応答では、取得した件数を一致件数とする。
	total := max(resp.Channel.TotalResults, len(resp.Channel.Items))
	truncated := total > len(resp.Channel.Items) || len(books) > maxResults
	if len(books) > maxResults {
		books = books[:maxResults]
	}
	return Result{Books: books, Total: total, Truncated: truncated}, nil
}

// titleRank は書名と検索語の一致度。小さいほど先に並べる。
func titleRank(title, query string) int {
	switch {
	case title == query:
		return 0
	case strings.HasPrefix(title, query):
		return 1
	default:
		return 2
	}
}

// 以下は OpenSearch の応答（RSS 2.0 + DC-NDL）のうち libread が使う要素。

type rssResponse struct {
	Channel struct {
		TotalResults int       `xml:"http://a9.com/-/spec/opensearchrss/1.0/ totalResults"`
		Items        []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Titles      []string     `xml:"http://purl.org/dc/elements/1.1/ title"`
	Creators    []string     `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Publishers  []string     `xml:"http://purl.org/dc/elements/1.1/ publisher"`
	Identifiers []identifier `xml:"http://purl.org/dc/elements/1.1/ identifier"`
}

type identifier struct {
	Type  string `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	Value string `xml:",chardata"`
}

// toDomain は ISBN として読める最初の識別子と書名を持つ記録だけを Book にする。
func (it rssItem) toDomain() (domain.Book, bool) {
	var isbn domain.ISBN
	for _, id := range it.Identifiers {
		if id.Type != "dcndl:ISBN" {
			continue
		}
		if i, err := domain.ParseISBN(strings.TrimSpace(id.Value)); err == nil {
			isbn = i
			break
		}
	}
	if isbn == "" || len(it.Titles) == 0 {
		return domain.Book{}, false
	}
	b, err := domain.NewBook(isbn, strings.TrimSpace(it.Titles[0]), joinTrimmed(it.Creators), joinTrimmed(it.Publishers))
	if err != nil {
		return domain.Book{}, false
	}
	return b, true
}

func joinTrimmed(ss []string) string {
	var out []string
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "、")
}

func (c *Client) get(ctx context.Context, q url.Values) (rssResponse, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return rssResponse{}, fmt.Errorf("国立国会図書館サーチへの要求を作れません: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return rssResponse{}, fmt.Errorf("国立国会図書館サーチに接続できません: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rssResponse{}, fmt.Errorf("国立国会図書館サーチから書誌を取得できません: %s", resp.Status)
	}
	var out rssResponse
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return rssResponse{}, fmt.Errorf("国立国会図書館サーチの応答の形式が正しくありません: %w", err)
	}
	return out, nil
}
