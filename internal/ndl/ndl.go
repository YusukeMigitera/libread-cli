// Package ndl は国立国会図書館サーチ OpenSearch API のクライアント（research.md R3）。
package ndl

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"libread/internal/domain"
)

// DefaultBaseURL は NDL サーチ OpenSearch API の URL。
const DefaultBaseURL = "https://ndlsearch.ndl.go.jp/api/opensearch"

const (
	// maxResults は1回の検索で返す候補の上限（contracts/cli.md の book find）。
	maxResults = 10
	// dataProvider は検索対象を国立国会図書館の蔵書に限る（research.md R3・R10）。
	dataProvider = "iss-ndl-opac"
)

// Client は NDL サーチ OpenSearch API のクライアント。
type Client struct {
	BaseURL   string // 空なら DefaultBaseURL
	HTTP      *http.Client
	UserAgent string
}

// Search は query を書名として（ISBN として読めるなら ISBN として）検索し、最大10件の書誌を返す。
// ISBN や書名のない記録は除き、同じ ISBN の記録は最初の1件だけを返す。
func (c *Client) Search(ctx context.Context, query string) ([]domain.Book, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("検索語が空です")
	}
	q := url.Values{
		"cnt":  {strconv.Itoa(maxResults)},
		"dpid": {dataProvider},
	}
	if isbn, err := domain.ParseISBN(query); err == nil {
		q.Set("isbn", isbn.String())
	} else {
		q.Set("title", query)
	}
	resp, err := c.get(ctx, q)
	if err != nil {
		return nil, err
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
		if len(books) == maxResults {
			break
		}
	}
	return books, nil
}

// 以下は OpenSearch の応答（RSS 2.0 + DC-NDL）のうち libread が使う要素。

type rssResponse struct {
	Channel struct {
		Items []rssItem `xml:"item"`
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
