// Package calil はカーリル 図書館API のクライアント（research.md R1）。
package calil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"libread/internal/domain"
)

// DefaultBaseURL はカーリル 図書館API の URL。
const DefaultBaseURL = "https://api.calil.jp"

const (
	// pollInterval はポーリングの間隔。カーリルの利用条件で2秒以上（research.md R1）。
	pollInterval = 2 * time.Second
	// checkDeadline はポーリングを含む問い合わせ全体の上限（research.md R7、SC-002）。
	checkDeadline = 8 * time.Second
)

// Client はカーリル 図書館API のクライアント。
type Client struct {
	BaseURL   string // 空なら DefaultBaseURL
	AppKey    string
	HTTP      *http.Client
	Now       func() time.Time                                 // nil なら time.Now
	Sleep     func(ctx context.Context, d time.Duration) error // nil なら実際に待つ
	UserAgent string
}

// Check は isbns × systemIDs の所蔵状況を1セッションで問い合わせる。
// 8秒の上限までに取得が終わらなかったシステムは、SystemStatus が Running のまま返す（取得タイムアウト）。
// 上限は注入した時計で測るので、テストでは実際に待たずに確かめられる。
func (c *Client) Check(ctx context.Context, isbns []domain.ISBN, systemIDs []string) ([]domain.HoldingSnapshot, error) {
	deadline := c.now().Add(checkDeadline)
	isbnStrs := make([]string, len(isbns))
	for i, isbn := range isbns {
		isbnStrs[i] = isbn.String()
	}
	resp, err := c.get(ctx, url.Values{
		"isbn":     {strings.Join(isbnStrs, ",")},
		"systemid": {strings.Join(systemIDs, ",")},
	})
	if err != nil {
		return nil, err
	}
	for resp.Continue == 1 && c.now().Add(pollInterval).Before(deadline) {
		if err := c.sleep(ctx, pollInterval); err != nil {
			return nil, fmt.Errorf("カーリルの応答を待てません: %w", err)
		}
		if resp, err = c.get(ctx, url.Values{"session": {resp.Session}}); err != nil {
			return nil, err
		}
	}

	fetchedAt := c.now()
	snaps := make([]domain.HoldingSnapshot, 0, len(isbns)*len(systemIDs))
	for _, isbn := range isbns {
		for _, system := range systemIDs {
			snap := domain.HoldingSnapshot{
				ISBN:         isbn,
				SystemID:     system,
				SystemStatus: domain.SystemError, // 応答になければ取得できなかったものとして扱う
				ByLibKey:     map[string]string{},
				FetchedAt:    fetchedAt,
			}
			if s, ok := resp.Books[isbn.String()][system]; ok {
				snap.SystemStatus = domain.SystemStatus(s.Status)
				for k, v := range s.LibKey {
					snap.ByLibKey[k] = v
				}
			}
			snaps = append(snaps, snap)
		}
	}
	return snaps, nil
}

// checkResponse は check API の応答（format=json&callback=no）。
type checkResponse struct {
	Session  string                            `json:"session"`
	Continue int                               `json:"continue"`
	Books    map[string]map[string]systemState `json:"books"` // ISBN → systemid → 状態
}

type systemState struct {
	Status     string            `json:"status"`
	ReserveURL string            `json:"reserveurl"`
	LibKey     map[string]string `json:"libkey"` // 館キー → 貸出状況
}

func (c *Client) get(ctx context.Context, q url.Values) (checkResponse, error) {
	q.Set("appkey", c.AppKey)
	q.Set("format", "json")
	q.Set("callback", "no")
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/check?"+q.Encode(), nil)
	if err != nil {
		return checkResponse{}, fmt.Errorf("カーリルへの要求を作れません: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return checkResponse{}, fmt.Errorf("カーリルに接続できません: %w", redactAppKey(err, c.AppKey))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return checkResponse{}, fmt.Errorf("カーリルから所蔵情報を取得できません: %s", resp.Status)
	}
	var out checkResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return checkResponse{}, fmt.Errorf("カーリルの応答の形式が正しくありません: %w", err)
	}
	return out, nil
}

func (c *Client) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// redactAppKey は、エラーに含まれる要求 URL からアプリケーションキーを伏せる（*url.Error は URL を含む）。
func redactAppKey(err error, key string) error {
	escaped := url.QueryEscape(key)
	if key == "" || !strings.Contains(err.Error(), escaped) {
		return err
	}
	return redactedError{msg: strings.ReplaceAll(err.Error(), escaped, "***"), err: err}
}

type redactedError struct {
	msg string
	err error
}

func (e redactedError) Error() string { return e.msg }
func (e redactedError) Unwrap() error { return e.err }
