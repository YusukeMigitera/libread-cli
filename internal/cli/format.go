package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"libread/internal/domain"
)

var weekdayNames = [7]string{"日", "月", "火", "水", "木", "金", "土"}

// writeResult は check の結果を contracts/cli.md「check の出力例」の形式で書く。
// books は調べた本、libs は利用図書館（登録順）。
func writeResult(w io.Writer, r domain.Result, books []domain.ReadingBook, libs []domain.RegisteredLibrary) {
	checked := r.CheckedAt.In(domain.Tokyo())
	fmt.Fprintf(w, "%s(%s) %s 時点\n", domain.DateOf(checked), weekdayNames[checked.Weekday()], clock(checked))

	fetchTimes := fetchTimesOf(r)
	perBook := len(fetchTimes) > 1

	fmt.Fprintln(w)
	fmt.Fprintln(w, "■ 今日読める図書館")
	if len(r.Readable) == 0 {
		fmt.Fprintln(w, "今日読める図書館はありません")
	}
	for i, l := range r.Readable {
		fmt.Fprintf(w, "%d. %s  %s\n", i+1, l.Library.Name, openingText(l.Opening))
		writeHoldings(w, l.Books, perBook)
	}

	if len(r.NotReadable) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "■ 今日は読めない図書館")
		for _, l := range r.NotReadable {
			fmt.Fprintf(w, "- %s  %s\n", l.Library.Name, openingText(l.Opening))
			// 今日行けない館は開館状態だけを理由として示し、本ごとの所蔵状況は省く。
			if s := l.Opening.Status; s == domain.Open || s == domain.NotYetOpen {
				writeHoldings(w, l.Books, perBook)
			}
		}
	}

	fmt.Fprintln(w)
	switch len(fetchTimes) {
	case 0:
		fmt.Fprintln(w, "所蔵情報: カーリル（最新の所蔵情報を取得できませんでした）")
	case 1:
		fmt.Fprintf(w, "所蔵情報: カーリル %s 取得（30分以内の取得結果を使用）\n", fetchTimes[0])
	default:
		fmt.Fprintln(w, "所蔵情報: カーリル（30分以内の取得結果を使用。取得時刻は本ごとに表示）")
	}
	for _, b := range books {
		fmt.Fprintf(w, "  『%s』 https://calil.jp/book/%s\n", b.Book.Title, b.Book.ISBN)
	}
	var updated []string
	for _, l := range libs {
		if l.Schedule != nil {
			updated = append(updated, l.ID.LibKey+" "+l.Schedule.UpdatedOn.String())
		}
	}
	if len(updated) == 0 {
		fmt.Fprintln(w, "開館情報: 利用者入力")
	} else {
		fmt.Fprintf(w, "開館情報: 利用者入力（最終更新 %s）\n", strings.Join(updated, " / "))
	}
	fmt.Fprintln(w, "※ 臨時休館は入力済みの情報のみ反映しています")
}

func writeHoldings(w io.Writer, holdings []domain.BookHolding, perBook bool) {
	for _, h := range holdings {
		fmt.Fprintf(w, "   - 『%s』 %s\n", h.Book.Title, holdingText(h, perBook))
	}
}

// fetchTimesOf は、最新とみなせる所蔵状況の取得時刻（HH:MM）を重複なく返す。
func fetchTimesOf(r domain.Result) []string {
	var times []string
	for _, group := range [][]domain.LibraryResult{r.Readable, r.NotReadable} {
		for _, l := range group {
			for _, h := range l.Books {
				if t := clock(h.FetchedAt); isFresh(h) && !slices.Contains(times, t) {
					times = append(times, t)
				}
			}
		}
	}
	return times
}

func isFresh(h domain.BookHolding) bool {
	return !h.Stale && (h.SystemStatus == domain.SystemOK || h.SystemStatus == domain.SystemCache)
}

func openingText(o domain.Opening) string {
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

func holdingText(h domain.BookHolding, perBook bool) string {
	switch {
	case h.Stale:
		return "不明（古い情報: " + clock(h.FetchedAt) + " 取得）"
	case h.SystemStatus == domain.SystemRunning:
		return "不明（取得タイムアウト）"
	case !isFresh(h):
		return "不明（取得失敗）"
	}
	var s string
	switch h.Availability {
	case domain.Reserved:
		s = h.Raw // FR-003: 予約中はカーリルの値をそのまま表示する
	case domain.Unknown:
		reason := h.Raw
		if reason == "蔵書あり" {
			reason = "蔵書あり／貸出状況不明"
		}
		s = "不明（" + reason + "）"
	default:
		s = h.Availability.Label()
	}
	if perBook {
		s += "（" + clock(h.FetchedAt) + " 取得）"
	}
	return s
}

// clock は t を日本時間の HH:MM で表す。
func clock(t time.Time) string { return t.In(domain.Tokyo()).Format("15:04") }
