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

	// realTimeout は実時間での上限（0 なら checkDeadline）。テストで短くする。
	realTimeout time.Duration
}

// Check は isbns × systemIDs の所蔵状況を1セッションで問い合わせる。
// 8秒の上限までに取得が終わらなかったシステムは、SystemStatus が Running のまま返す（取得タイムアウト）。
// ポーリングが失敗したときも、それまでに取得できた結果を返し、未完了のシステムは取得タイムアウトとする。
// エラーを返すのは、初回の要求が失敗したときと、ctx が取り消されたときだけ。
//
// ポーリングを続けるかは注入した時計で判定するので、テストでは実際に待たずに確かめられる。
// HTTP の待ち時間は実時間でしか測れないため、要求には実時間で上限に切れる context も渡す（SC-002）。
func (c *Client) Check(ctx context.Context, isbns []domain.ISBN, systemIDs []string) ([]domain.HoldingSnapshot, error) {
	deadline := c.now().Add(checkDeadline)
	timeout := c.realTimeout
	if timeout == 0 {
		timeout = checkDeadline
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	isbnStrs := make([]string, len(isbns))
	for i, isbn := range isbns {
		isbnStrs[i] = isbn.String()
	}
	resp, err := c.get(reqCtx, url.Values{
		"isbn":     {strings.Join(isbnStrs, ",")},
		"systemid": {strings.Join(systemIDs, ",")},
	})
	if err != nil {
		return nil, err
	}
	for resp.Continue == 1 && c.now().Add(pollInterval).Before(deadline) {
		if err := c.sleep(reqCtx, pollInterval); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("カーリルの応答を待てません: %w", ctx.Err())
			}
			break // 実時間の上限に達した
		}
		next, err := c.get(reqCtx, url.Values{"session": {resp.Session}})
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			break // 実時間の上限に達したか、ポーリングが失敗した。取得済みの結果を返す
		}
		resp = next
	}

	fetchedAt := c.now()
	// 応答にない本・システムは、取得中に打ち切ったならタイムアウト、取得が終わっていたなら取得できなかったものとして扱う。
	// 実際の応答では、取得中の本が books に載っていないことがある。
	missing := domain.SystemError
	if resp.Continue == 1 {
		missing = domain.SystemRunning
	}
	snaps := make([]domain.HoldingSnapshot, 0, len(isbns)*len(systemIDs))
	for _, isbn := range isbns {
		for _, system := range systemIDs {
			snap := domain.HoldingSnapshot{
				ISBN:         isbn,
				SystemID:     system,
				SystemStatus: missing,
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
