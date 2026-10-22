package domain

import (
	"slices"
	"testing"
)

func TestReadable(t *testing.T) {
	opens := []OpenStatus{Open, NotYetOpen, FinishedToday, ClosedRegular, ClosedPeriod, NoSchedule, HolidayUnknown}
	avails := []Availability{AvailableToLend, InLibraryOnly, OnLoan, Reserved, NotHeld, Unknown}
	for _, o := range opens {
		for _, a := range avails {
			for _, stale := range []bool{false, true} {
				want := (o == Open || o == NotYetOpen) && (a == AvailableToLend || a == InLibraryOnly) && !stale
				if got := Readable(o, a, stale); got != want {
					t.Errorf("Readable(%v, %v, stale=%v) = %v, want %v", o, a, stale, got, want)
				}
			}
		}
	}
}

func TestNewResult(t *testing.T) {
	checkedAt := at(14, 5, 0)
	bookP := Book{ISBN: "9784003101018", Title: "P"}
	bookQ := Book{ISBN: "9784003101100", Title: "Q"}
	holding := func(b Book, a Availability, stale bool) BookHolding {
		return BookHolding{Book: b, Availability: a, Stale: stale, SystemStatus: SystemOK, FetchedAt: checkedAt}
	}
	lib := func(key string, o Opening, books ...BookHolding) LibraryResult {
		return LibraryResult{
			Library: RegisteredLibrary{ID: LibraryID{SystemID: "S", LibKey: key}, Name: key},
			Opening: o,
			Books:   books,
		}
	}
	open := func(closeAt string) Opening {
		return Opening{Status: Open, Hours: mustHours(t, "09:00", closeAt)}
	}
	notYet := func(openAt, closeAt string) Opening {
		return Opening{Status: NotYetOpen, Hours: mustHours(t, openAt, closeAt)}
	}

	libs := []LibraryResult{
		lib("1冊・19時", open("19:00"), holding(bookP, AvailableToLend, false), holding(bookQ, OnLoan, false)),
		lib("休館", Opening{Status: ClosedRegular}, holding(bookP, AvailableToLend, false), holding(bookQ, AvailableToLend, false)),
		lib("2冊・17時", open("17:00"), holding(bookP, AvailableToLend, false), holding(bookQ, InLibraryOnly, false)),
		lib("古い情報", open("20:00"), holding(bookP, AvailableToLend, true), holding(bookQ, Unknown, false)),
		lib("1冊・開館前20時", notYet("15:00", "20:00"), holding(bookP, InLibraryOnly, false), holding(bookQ, Reserved, false)),
		lib("未入力", Opening{Status: NoSchedule}),
		lib("1冊・14時半", open("14:30"), holding(bookP, AvailableToLend, false), holding(bookQ, NotHeld, false)),
		lib("終了", Opening{Status: FinishedToday, Hours: mustHours(t, "09:00", "14:00")}, holding(bookP, AvailableToLend, false)),
	}

	got := NewResult(checkedAt, libs)

	if !got.CheckedAt.Equal(checkedAt) {
		t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, checkedAt)
	}
	wantReadable := []string{"2冊・17時", "1冊・開館前20時", "1冊・19時", "1冊・14時半"}
	wantNotReadable := []string{"休館", "古い情報", "未入力", "終了"}
	if names := libNames(got.Readable); !slices.Equal(names, wantReadable) {
		t.Errorf("Readable = %v, want %v", names, wantReadable)
	}
	if names := libNames(got.NotReadable); !slices.Equal(names, wantNotReadable) {
		t.Errorf("NotReadable = %v, want %v", names, wantNotReadable)
	}
	// 理由を表示できるよう、読めない館も開館状態と本ごとの所蔵状況を保持する。
	for _, r := range got.NotReadable {
		if r.Library.Name == "古い情報" {
			if len(r.Books) != 2 || !r.Books[0].Stale || r.Opening.Status != Open {
				t.Errorf("古い情報の館の内容が失われている: %+v", r)
			}
		}
	}
	if n := got.Readable[0].ReadableCount(); n != 2 {
		t.Errorf("ReadableCount() = %d, want 2", n)
	}
}

func TestNewResultNoneReadable(t *testing.T) {
	libs := []LibraryResult{
		{Library: RegisteredLibrary{Name: "A"}, Opening: Opening{Status: ClosedRegular}},
		{Library: RegisteredLibrary{Name: "B"}, Opening: Opening{Status: NoSchedule}},
	}
	got := NewResult(at(14, 5, 0), libs)
	if len(got.Readable) != 0 {
		t.Errorf("Readable = %v, want empty", libNames(got.Readable))
	}
	if names := libNames(got.NotReadable); !slices.Equal(names, []string{"A", "B"}) {
		t.Errorf("NotReadable = %v", names)
	}
}

func libNames(rs []LibraryResult) []string {
	names := make([]string, 0, len(rs))
	for _, r := range rs {
		names = append(names, r.Library.Name)
	}
	return names
}
