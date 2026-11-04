package domain

import "time"

// Availability はある館でのある本の所蔵状況（FR-003）。ゼロ値は「不明」。
type Availability int

const (
	Unknown Availability = iota
	AvailableToLend
	InLibraryOnly
	OnLoan
	Reserved
	NotHeld
)

// Label は表示用の分類名。予約中はカーリルの値（予約中）をそのまま使う。
func (a Availability) Label() string {
	switch a {
	case AvailableToLend:
		return "在架（貸出可）"
	case InLibraryOnly:
		return "在架（館内閲覧のみ）"
	case OnLoan:
		return "貸出中"
	case Reserved:
		return "予約中"
	case NotHeld:
		return "所蔵なし"
	default:
		return "不明"
	}
}

func (a Availability) String() string { return a.Label() }

// SystemStatus はカーリルが図書館システム単位で返す取得状況。
type SystemStatus string

const (
	SystemOK      SystemStatus = "OK"
	SystemCache   SystemStatus = "Cache"
	SystemRunning SystemStatus = "Running"
	SystemError   SystemStatus = "Error"
)

// HoldingSnapshot は1冊・1図書館システム分の所蔵状況。ByLibKey はカーリルが返した元の値。
type HoldingSnapshot struct {
	ISBN         ISBN
	SystemID     string
	SystemStatus SystemStatus
	ByLibKey     map[string]string
	FetchedAt    time.Time
}

// FreshFor は所蔵状況を最新とみなせる期間（spec Assumptions、research.md R7）。
const FreshFor = 30 * time.Minute

// IsStale は now の時点で s が FreshFor を過ぎているかを返す。
func (s HoldingSnapshot) IsStale(now time.Time) bool {
	return now.Sub(s.FetchedAt) > FreshFor
}

// AvailabilityOf はスナップショットから館 libKey の所蔵状況を求める（research.md R2）。
// raw はカーリルが返した元の値（館が応答になければ空）。
func AvailabilityOf(s HoldingSnapshot, libKey string, now time.Time) (a Availability, raw string, stale bool) {
	raw, found := s.ByLibKey[libKey]
	stale = s.IsStale(now)
	if s.SystemStatus != SystemOK && s.SystemStatus != SystemCache {
		return Unknown, raw, stale
	}
	if !found {
		return NotHeld, raw, stale
	}
	switch raw {
	case "貸出可":
		return AvailableToLend, raw, stale
	case "館内のみ":
		return InLibraryOnly, raw, stale
	case "貸出中":
		return OnLoan, raw, stale
	case "予約中":
		return Reserved, raw, stale
	case "蔵書なし":
		return NotHeld, raw, stale
	default:
		return Unknown, raw, stale
	}
}
