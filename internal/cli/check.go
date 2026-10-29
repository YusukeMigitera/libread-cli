package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"libread/internal/calil"
	"libread/internal/domain"
	"libread/internal/holiday"
	"libread/internal/store"
)

// version は User-Agent に付ける libread のバージョン（research.md R9）。
const version = "0.1.0"

const userAgent = "libread/" + version

// holdingsCacheName は所蔵状況のキャッシュ。キーは ISBN|SystemID（data-model.md）。
const holdingsCacheName = "holdings.json"

var checkCommand = command{
	name:    "check",
	summary: "今日読める図書館を表示する（引数なしの libread と同じ）。check <本> でその本だけを調べる",
	run:     runCheck,
}

func runCheck(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return usageErrorf("使い方: libread check [<本>]")
	}
	if fs.NArg() > 1 {
		return usageErrorf("使い方: libread check [<本>]")
	}

	data, err := store.Load(env.DataDir)
	if err != nil {
		return err
	}
	if len(data.Libraries) == 0 {
		return usageErrorf("利用図書館が登録されていません。libread lib find --pref <都道府県> で図書館を探し、libread lib add で登録してください")
	}
	if len(data.Books) == 0 {
		return usageErrorf("読みかけの本が登録されていません。libread book find <書名> で本を探し、libread book add で登録してください")
	}
	books := data.Books
	if fs.NArg() == 1 {
		b, err := resolveBook(data.Books, fs.Arg(0))
		if err != nil {
			return err
		}
		books = []domain.ReadingBook{b}
	}
	if err := requireAppKey(env); err != nil {
		return err
	}

	now := env.Now()
	cache := store.NewCache(env.CacheDir)
	holdings, err := loadHoldings(ctx, env, cache, books, systemIDsOf(data.Libraries), now)
	if err != nil {
		return err
	}
	isHoliday := (&holiday.Client{
		HTTP:      env.HTTPClient,
		Cache:     cache,
		Now:       env.Now,
		UserAgent: userAgent,
	}).Lookup(ctx)

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
	writeResult(env.Stdout, domain.NewResult(now, libs), books, data.Libraries)
	return nil
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

// loadHoldings は books × systemIDs の所蔵状況を返す（research.md R7）。
//   - すべて30分以内のキャッシュがあれば、カーリルに問い合わせない。
//   - そうでなければ1セッションで取得し直し、取得できたもの（OK・Cache）をキャッシュに保存する。
//   - 取得できなかったものは、古くてもキャッシュがあればそれを使う（表示は「古い情報」になる）。
//
// どの組み合わせにも使える所蔵状況がなければ、エラーを返す。
func loadHoldings(ctx context.Context, env Env, cache store.Cache, books []domain.ReadingBook, systemIDs []string, now time.Time) (map[string]domain.HoldingSnapshot, error) {
	cached, err := readHoldingsCache(cache)
	if err != nil {
		fmt.Fprintf(env.Stderr, "libread: 警告: %v（取得し直します）\n", err)
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

	client := &calil.Client{
		AppKey:    env.CalilAppKey,
		HTTP:      env.HTTPClient,
		Now:       env.Now,
		Sleep:     env.Sleep,
		UserAgent: userAgent,
	}
	fetched, fetchErr := client.Check(ctx, isbns, systemIDs)
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
		fmt.Fprintf(env.Stderr, "libread: 警告: %v（取得済みの古い情報を表示します）\n", fetchErr)
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
	if err := saveHoldingsCache(cache, cached, now); err != nil {
		fmt.Fprintf(env.Stderr, "libread: 警告: %v\n", err)
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
