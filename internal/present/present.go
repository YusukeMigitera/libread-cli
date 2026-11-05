// Package present は、開館状態・所蔵状況・出典の表示文言を作る（research W5）。
// 001 の CLI の出力（specs/001-readable-libraries-today/contracts/cli.md）と Web の API が同じ文言を使う。
package present

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"libread/internal/domain"
)

// 出典表示と注意書き（001 research R10、002 contracts/http-api.md）。
const (
	ClosureNote = "※ 臨時休館は入力済みの情報のみ反映しています"

	// NDL サーチの利用条件で必須の出典表示。iss-ndl-opac のメタデータは CC BY なので、
	// データプロバイダとデータ提供機関の名前、ライセンスへのリンクを示す。
	NDLCreditLabel  = "書誌情報: 国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータを利用しています"
	NDLURL          = "https://ndlsearch.ndl.go.jp/"
	NDLLicenseLabel = "クリエイティブ・コモンズ 表示 4.0 国際"
	NDLLicenseURL   = "https://creativecommons.org/licenses/by/4.0/legalcode.ja"

	CalilCreditLabel = "図書館情報: カーリル"
	CalilURL         = "https://calil.jp/"

	HolidaysLabel = "祝日: holidays-jp（https://holidays-jp.github.io/）"
)

// CalilBookURL はカーリルの本のページ（所蔵情報の出典のリンク）。
func CalilBookURL(isbn domain.ISBN) string { return CalilURL + "book/" + isbn.String() }

// OpeningLabel は今日の開館状態の文言（例: 「開館中 〜19:00」「開館前 15:00〜20:00」）。
func OpeningLabel(o domain.Opening) string {
	switch o.Status {
	case domain.Open:
		s := "開館中 〜" + o.Hours.Close().String()
		if o.ClosingSoon {
			s += "（閉館まで30分未満）"
		}
		return s
	case domain.NotYetOpen:
		return "開館前 " + o.Hours.Open().String() + "〜" + o.Hours.Close().String()
	case domain.FinishedToday:
		return "本日の開館は終了"
	case domain.ClosedRegular:
		return "休館（定休日）"
	case domain.ClosedPeriod:
		reason := o.Closure.Reason()
		if reason == "" {
			reason = "臨時休館"
		}
		return fmt.Sprintf("休館（%s %s〜%s）", reason, o.Closure.Start(), o.Closure.End())
	case domain.HolidayUnknown:
		return "開館情報不明（祝日判定不可）"
	default:
		return "開館情報未入力"
	}
}

// AvailabilityLabel はある館でのある本の所蔵状況の文言（例: 「在架（貸出可）」「不明（古い情報: 13:00 取得）」）。
// 本ごとの取得時刻は付けない。
func AvailabilityLabel(h domain.BookHolding) string {
	switch {
	case h.Stale:
		return "不明（古い情報: " + Clock(h.FetchedAt) + " 取得）"
	case h.SystemStatus == domain.SystemRunning:
		return "不明（取得タイムアウト）"
	case !IsFresh(h):
		return "不明（取得失敗）"
	}
	switch h.Availability {
	case domain.Reserved:
		return h.Raw // 001 FR-003: 予約中はカーリルの値をそのまま表示する
	case domain.Unknown:
		reason := h.Raw
		if reason == "蔵書あり" {
			reason = "蔵書あり／貸出状況不明"
		}
		return "不明（" + reason + "）"
	default:
		return h.Availability.Label()
	}
}

// IsFresh は、h が30分以内に取得できた所蔵状況か。
func IsFresh(h domain.BookHolding) bool {
	return !h.Stale && (h.SystemStatus == domain.SystemOK || h.SystemStatus == domain.SystemCache)
}

// FetchTimes は、最新とみなせる所蔵状況の取得時刻（HH:MM）を重複なく返す。
func FetchTimes(r domain.Result) []string {
	var times []string
	for _, group := range [][]domain.LibraryResult{r.Readable, r.NotReadable} {
		for _, l := range group {
			for _, h := range l.Books {
				if t := Clock(h.FetchedAt); IsFresh(h) && !slices.Contains(times, t) {
					times = append(times, t)
				}
			}
		}
	}
	return times
}

// HoldingsSourceLabel は所蔵情報の出典の文言。
func HoldingsSourceLabel(r domain.Result) string {
	times := FetchTimes(r)
	switch len(times) {
	case 0:
		return "所蔵情報: カーリル（最新の所蔵情報を取得できませんでした）"
	case 1:
		return "所蔵情報: カーリル " + times[0] + " 取得（30分以内の取得結果を使用）"
	default:
		return "所蔵情報: カーリル（30分以内の取得結果を使用。取得時刻は本ごとに表示）"
	}
}

// OpeningInfoSourceLabel は開館情報の出典の文言。libs は利用図書館（登録順）。
func OpeningInfoSourceLabel(libs []domain.RegisteredLibrary) string {
	var updated []string
	for _, l := range libs {
		if l.Schedule != nil {
			updated = append(updated, l.ID.LibKey+" "+l.Schedule.UpdatedOn.String())
		}
	}
	if len(updated) == 0 {
		return "開館情報: 利用者入力"
	}
	return "開館情報: 利用者入力（最終更新 " + strings.Join(updated, " / ") + "）"
}

// Clock は t を日本時間の HH:MM で表す。
func Clock(t time.Time) string { return t.In(domain.Tokyo()).Format("15:04") }
