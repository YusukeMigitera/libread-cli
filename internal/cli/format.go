package cli

import (
	"fmt"
	"io"

	"libread/internal/domain"
	"libread/internal/present"
)

var weekdayNames = [7]string{"日", "月", "火", "水", "木", "金", "土"}

// writeResult は check の結果を contracts/cli.md「check の出力例」の形式で書く。
// books は調べた本、libs は利用図書館（登録順）。文言は internal/present で作る（Web の API と共通）。
func writeResult(w io.Writer, r domain.Result, books []domain.ReadingBook, libs []domain.RegisteredLibrary) {
	checked := r.CheckedAt.In(domain.Tokyo())
	fmt.Fprintf(w, "%s(%s) %s 時点\n", domain.DateOf(checked), weekdayNames[checked.Weekday()], present.Clock(checked))

	perBook := len(present.FetchTimes(r)) > 1

	fmt.Fprintln(w)
	fmt.Fprintln(w, "■ 今日読める図書館")
	if len(r.Readable) == 0 {
		fmt.Fprintln(w, "今日読める図書館はありません")
	}
	for i, l := range r.Readable {
		fmt.Fprintf(w, "%d. %s  %s\n", i+1, l.Library.Name, present.OpeningLabel(l.Opening))
		writeHoldings(w, l.Books, perBook)
	}

	if len(r.NotReadable) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "■ 今日は読めない図書館")
		for _, l := range r.NotReadable {
			fmt.Fprintf(w, "- %s  %s\n", l.Library.Name, present.OpeningLabel(l.Opening))
			// 今日行けない館は開館状態だけを理由として示し、本ごとの所蔵状況は省く。
			if s := l.Opening.Status; s == domain.Open || s == domain.NotYetOpen {
				writeHoldings(w, l.Books, perBook)
			}
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, present.HoldingsSourceLabel(r))
	for _, b := range books {
		fmt.Fprintf(w, "  『%s』 %s\n", b.Book.Title, present.CalilBookURL(b.Book.ISBN))
	}
	fmt.Fprintln(w, present.OpeningInfoSourceLabel(libs))
	fmt.Fprintln(w, present.ClosureNote)
}

func writeHoldings(w io.Writer, holdings []domain.BookHolding, perBook bool) {
	for _, h := range holdings {
		fmt.Fprintf(w, "   - 『%s』 %s\n", h.Book.Title, holdingText(h, perBook))
	}
}

// holdingText は所蔵状況の文言。取得時刻が本ごとに違うときは、取得できた本に取得時刻を付ける。
func holdingText(h domain.BookHolding, perBook bool) string {
	s := present.AvailabilityLabel(h)
	if perBook && present.IsFresh(h) {
		s += "（" + present.Clock(h.FetchedAt) + " 取得）"
	}
	return s
}
