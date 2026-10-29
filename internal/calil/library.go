package calil

import (
	"context"
	"errors"
	"net/url"

	"libread/internal/domain"
)

// LibraryInfo はカーリルの図書館検索で得た図書館1館。
type LibraryInfo struct {
	ID      domain.LibraryID
	Formal  string // 正式名
	Pref    string
	City    string
	Address string
}

// Libraries は都道府県（必須）と市区町村（空なら指定しない）で図書館を探す（FR-011）。
func (c *Client) Libraries(ctx context.Context, pref, city string) ([]LibraryInfo, error) {
	if pref == "" {
		return nil, errors.New("図書館を探すには都道府県を指定してください")
	}
	q := url.Values{"pref": {pref}}
	if city != "" {
		q.Set("city", city)
	}
	return c.libraries(ctx, q)
}

// LibrariesOfSystem は図書館システムに属する図書館を返す。登録時に SystemID:LibKey を確かめるのに使う。
func (c *Client) LibrariesOfSystem(ctx context.Context, systemID string) ([]LibraryInfo, error) {
	return c.libraries(ctx, url.Values{"systemid": {systemID}})
}

func (c *Client) libraries(ctx context.Context, q url.Values) ([]LibraryInfo, error) {
	q.Set("callback", "") // 空なら JSONP ではなく JSON で返る
	raw, err := getJSON[[]libraryJSON](ctx, c, "/library", q, "図書館の一覧")
	if err != nil {
		return nil, err
	}
	out := make([]LibraryInfo, 0, len(raw))
	for _, l := range raw {
		id, err := domain.ParseLibraryID(l.SystemID + ":" + l.LibKey)
		if err != nil {
			continue // ID の欠けた館は登録も所蔵確認もできないので飛ばす。他の館は使えるようにする
		}
		out = append(out, LibraryInfo{ID: id, Formal: l.Formal, Pref: l.Pref, City: l.City, Address: l.Address})
	}
	return out, nil
}

// libraryJSON は library API の応答（format=json）の1館。使う項目だけを読む。
type libraryJSON struct {
	SystemID string `json:"systemid"`
	LibKey   string `json:"libkey"`
	Formal   string `json:"formal"`
	Pref     string `json:"pref"`
	City     string `json:"city"`
	Address  string `json:"address"`
}
