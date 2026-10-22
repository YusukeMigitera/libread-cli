// Package holiday は holidays-jp API で日本の祝日を判定する（research.md R4）。
package holiday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"libread/internal/domain"
	"libread/internal/store"
)

// DefaultBaseURL は holidays-jp API の URL。
const DefaultBaseURL = "https://holidays-jp.github.io/api/v1"

// refreshAfter を過ぎた年のキャッシュは取得し直す（research.md R9）。
const refreshAfter = 30 * 24 * time.Hour

// Client は年ごとの祝日一覧を取得し、キャッシュする。
type Client struct {
	BaseURL   string // 空なら DefaultBaseURL
	HTTP      *http.Client
	Cache     store.Cache
	Now       func() time.Time
	UserAgent string
}

// Lookup は日付が祝日か（isHoliday）と、それを判定できたか（known）を返す関数を返す。
// 祝日一覧は最初に必要になったときに年ごとに1回だけ読み込む。
func (c *Client) Lookup(ctx context.Context) func(domain.Date) (isHoliday, known bool) {
	years := map[int]map[string]string{} // 判定できない年は nil
	return func(d domain.Date) (bool, bool) {
		year := d.Year()
		holidays, loaded := years[year]
		if !loaded {
			holidays = c.load(ctx, year)
			years[year] = holidays
		}
		if holidays == nil {
			return false, false
		}
		_, ok := holidays[d.String()]
		return ok, true
	}
}

// load は year の祝日一覧（日付 → 祝日名）を返す。30日以内のキャッシュがあればそれを使う。
// 取得に失敗したときは古いキャッシュを使い、それもなければ nil を返す（判定不可）。
func (c *Client) load(ctx context.Context, year int) map[string]string {
	name := "holidays-" + strconv.Itoa(year) + ".json"
	cached, storedAt, ok, err := store.Get[map[string]string](c.Cache, name)
	if err != nil {
		ok = false // 壊れたキャッシュは無いものとして取得し直す
	}
	now := c.Now()
	if ok && now.Sub(storedAt) <= refreshAfter {
		return cached
	}
	fetched, err := c.fetch(ctx, year)
	if err != nil {
		if ok {
			return cached
		}
		return nil
	}
	// 保存に失敗しても、取得した一覧で今回の判定はできる。次回は取得し直す。
	_ = store.Put(c.Cache, name, fetched, now)
	return fetched
}

func (c *Client) fetch(ctx context.Context, year int) (map[string]string, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := fmt.Sprintf("%s/%d/date.json", base, year)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("祝日データの要求を作れません: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("祝日データを取得できません: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("祝日データを取得できません: %s", resp.Status)
	}
	var holidays map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&holidays); err != nil {
		return nil, fmt.Errorf("祝日データの形式が正しくありません: %w", err)
	}
	return holidays, nil
}
