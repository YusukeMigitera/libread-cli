// Package checkrun は、所蔵状況の取得（キャッシュ込み）と「読める」の判定を組み立てる。
// 001 の CLI（internal/cli）と Web のサーバー（internal/server）が共通で使う。
package checkrun

import (
	"context"
	"fmt"
	"sync"
	"time"

	"libread/internal/calil"
	"libread/internal/domain"
	"libread/internal/holiday"
	"libread/internal/store"
)

// holdingsCacheName は所蔵状況のキャッシュ。キーは ISBN|SystemID（001 data-model.md）。
const holdingsCacheName = "holdings.json"

// Deps は検索に使う外部サービスとキャッシュ。
type Deps struct {
	Calil    *calil.Client
	Holidays *holiday.Client
	Cache    store.Cache
	// CalilLock は、所蔵キャッシュを読むところからカーリルに問い合わせて保存するところまで持つロック。
	// nil ならロックしない。祝日の取得や判定のあいだは持たない（002 research W9）。
	CalilLock sync.Locker
	// Warn は、検索は続けられるが利用者に知らせたいこと（キャッシュの読み書きの失敗、古い情報での表示）を受け取る。
	Warn func(string)
}

// Run は books について、data の利用図書館それぞれで今日読めるかを now の時点で判定する。
// 所蔵状況は30分以内のキャッシュを使い、なければカーリルに1セッションで問い合わせる（001 research R7）。
// どの組み合わせにも使える所蔵状況がないときだけエラーを返す。
func Run(ctx context.Context, deps Deps, data store.Data, books []domain.ReadingBook, now time.Time) (domain.Result, error) {
	if deps.CalilLock != nil {
		deps.CalilLock.Lock()
	}
	holdings, err := loadHoldings(ctx, deps, books, systemIDsOf(data.Libraries), now)
	if deps.CalilLock != nil {
		deps.CalilLock.Unlock()
	}
	if err != nil {
		return domain.Result{}, err
	}
	isHoliday := deps.Holidays.Lookup(ctx)

	libs := make([]domain.LibraryResult, 0, len(data.Libraries))
	for _, lib := range data.Libraries {
		r := domain.LibraryResult{Library: lib, Opening: domain.OpenStatusOf(lib, now, isHoliday)}
		for _, b := range books {
			snap := holdings[holdingKey(b.Book.ISBN, lib.ID.SystemID)]
			avail, raw, stale := domain.AvailabilityOf(snap, lib.ID.LibKey, now)
			r.Books = append(r.Books, domain.BookHolding{
				Book:         b.Book,
				Availability: avail,
				Raw:          raw,
				Stale:        stale,
				SystemStatus: snap.SystemStatus,
				FetchedAt:    snap.FetchedAt,
			})
		}
		libs = append(libs, r)
	}
	return domain.NewResult(now, libs), nil
}

func (d Deps) warn(format string, a ...any) {
	if d.Warn != nil {
		d.Warn(fmt.Sprintf(format, a...))
	}
}

// systemIDsOf は図書館システムIDを重複なく登録順に返す。
func systemIDsOf(libs []domain.RegisteredLibrary) []string {
	var ids []string
	seen := map[string]bool{}
	for _, l := range libs {
		if !seen[l.ID.SystemID] {
			seen[l.ID.SystemID] = true
			ids = append(ids, l.ID.SystemID)
		}
	}
	return ids
}

func holdingKey(isbn domain.ISBN, systemID string) string {
	return isbn.String() + "|" + systemID
}

// loadHoldings は books × systemIDs の所蔵状況を返す（001 research.md R7）。
//   - すべて30分以内のキャッシュがあれば、カーリルに問い合わせない。
//   - そうでなければ1セッションで取得し直し、取得できたもの（OK・Cache）をキャッシュに保存する。
//   - 取得できなかったものは、古くてもキャッシュがあればそれを使う（表示は「古い情報」になる）。
//
// どの組み合わせにも使える所蔵状況がなければ、エラーを返す。
func loadHoldings(ctx context.Context, deps Deps, books []domain.ReadingBook, systemIDs []string, now time.Time) (map[string]domain.HoldingSnapshot, error) {
	cached, err := readHoldingsCache(deps.Cache)
	if err != nil {
		deps.warn("%v（取得し直します）", err)
		cached = map[string]domain.HoldingSnapshot{}
	}

	isbns := make([]domain.ISBN, len(books))
	for i, b := range books {
		isbns[i] = b.Book.ISBN
	}
	out := map[string]domain.HoldingSnapshot{}
	allFresh := true
	for _, isbn := range isbns {
		for _, system := range systemIDs {
			k := holdingKey(isbn, system)
			if s, ok := cached[k]; ok && !s.IsStale(now) {
				out[k] = s
			} else {
				allFresh = false
			}
		}
	}
	if allFresh {
		return out, nil
	}

	fetched, fetchErr := deps.Calil.Check(ctx, isbns, systemIDs)
	if fetchErr != nil {
		usable := false
		for _, isbn := range isbns {
			for _, system := range systemIDs {
				k := holdingKey(isbn, system)
				if s, ok := cached[k]; ok {
					out[k] = s
					usable = true
				} else {
					out[k] = domain.HoldingSnapshot{ISBN: isbn, SystemID: system, SystemStatus: domain.SystemError, FetchedAt: now}
				}
			}
		}
		if !usable {
			return nil, fetchErr
		}
		deps.warn("%v（取得済みの古い情報を表示します）", fetchErr)
		return out, nil
	}

	for _, s := range fetched {
		k := holdingKey(s.ISBN, s.SystemID)
		if s.SystemStatus == domain.SystemOK || s.SystemStatus == domain.SystemCache {
			out[k] = s
			cached[k] = s
		} else if c, ok := cached[k]; ok {
			out[k] = c
		} else {
			out[k] = s
		}
	}
	if err := saveHoldingsCache(deps.Cache, cached, now); err != nil {
		deps.warn("%v", err)
	}
	return out, nil
}

// holdingJSON は holdings.json に保存する1件分の所蔵状況。
type holdingJSON struct {
	ISBN         string            `json:"isbn"`
	SystemID     string            `json:"systemId"`
	SystemStatus string            `json:"systemStatus"`
	ByLibKey     map[string]string `json:"byLibKey"`
	FetchedAt    time.Time         `json:"fetchedAt"`
}

func readHoldingsCache(cache store.Cache) (map[string]domain.HoldingSnapshot, error) {
	raw, _, ok, err := store.Get[map[string]holdingJSON](cache, holdingsCacheName)
	out := map[string]domain.HoldingSnapshot{}
	if err != nil || !ok {
		return out, err
	}
	for k, h := range raw {
		isbn, err := domain.ParseISBN(h.ISBN)
		if err != nil {
			return map[string]domain.HoldingSnapshot{}, fmt.Errorf("所蔵キャッシュ %s: %w", k, err)
		}
		out[k] = domain.HoldingSnapshot{
			ISBN:         isbn,
			SystemID:     h.SystemID,
			SystemStatus: domain.SystemStatus(h.SystemStatus),
			ByLibKey:     h.ByLibKey,
			FetchedAt:    h.FetchedAt,
		}
	}
	return out, nil
}

func saveHoldingsCache(cache store.Cache, snaps map[string]domain.HoldingSnapshot, now time.Time) error {
	raw := make(map[string]holdingJSON, len(snaps))
	for k, s := range snaps {
		raw[k] = holdingJSON{
			ISBN:         s.ISBN.String(),
			SystemID:     s.SystemID,
			SystemStatus: string(s.SystemStatus),
			ByLibKey:     s.ByLibKey,
			FetchedAt:    s.FetchedAt,
		}
	}
	return store.Put(cache, holdingsCacheName, raw, now)
}
