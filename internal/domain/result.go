package domain

import (
	"cmp"
	"slices"
	"time"
)

// Readable は、開館状態と所蔵状況から今日その館で本を読めるかを返す（FR-004、原則 IV）。
func Readable(open OpenStatus, avail Availability, stale bool) bool {
	return (open == Open || open == NotYetOpen) &&
		(avail == AvailableToLend || avail == InLibraryOnly) &&
		!stale
}

// BookHolding はある館でのある本の所蔵状況。
type BookHolding struct {
	Book         Book
	Availability Availability
	Raw          string // カーリルが返した元の値
	Stale        bool   // 取得から30分を過ぎている
	SystemStatus SystemStatus
	FetchedAt    time.Time
}

// LibraryResult は1館分の検索結果。
type LibraryResult struct {
	Library RegisteredLibrary
	Opening Opening
	Books   []BookHolding
}

// ReadableCount は今日この館で読める本の冊数。
func (r LibraryResult) ReadableCount() int {
	n := 0
	for _, b := range r.Books {
		if Readable(r.Opening.Status, b.Availability, b.Stale) {
			n++
		}
	}
	return n
}

// Result は検索結果を「読める館」と「読めない館」に分けたもの。
type Result struct {
	CheckedAt   time.Time
	Readable    []LibraryResult // 読める冊数の多い順 → 閉館時刻の遅い順（FR-008）
	NotReadable []LibraryResult // 登録順
}

// NewResult は libs（登録順）を読める館と読めない館に分け、読める館を FR-008 の順に並べる。
// 同順位の館は登録順のままにする。
func NewResult(checkedAt time.Time, libs []LibraryResult) Result {
	r := Result{CheckedAt: checkedAt}
	for _, l := range libs {
		if l.ReadableCount() > 0 {
			r.Readable = append(r.Readable, l)
		} else {
			r.NotReadable = append(r.NotReadable, l)
		}
	}
	slices.SortStableFunc(r.Readable, func(a, b LibraryResult) int {
		if c := cmp.Compare(b.ReadableCount(), a.ReadableCount()); c != 0 {
			return c
		}
		return cmp.Compare(b.Opening.Hours.Close(), a.Opening.Hours.Close())
	})
	return r
}
