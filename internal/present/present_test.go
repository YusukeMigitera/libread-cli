package present

import (
	"testing"
	"time"

	"libread/internal/domain"
)

func mustHours(t *testing.T, open, close string) domain.Hours {
	t.Helper()
	o, err := domain.ParseTimeOfDay(open)
	if err != nil {
		t.Fatal(err)
	}
	c, err := domain.ParseTimeOfDay(close)
	if err != nil {
		t.Fatal(err)
	}
	h, err := domain.NewHours(o, c)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustBook(t *testing.T, isbn, title string) domain.Book {
	t.Helper()
	i, err := domain.ParseISBN(isbn)
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.NewBook(i, title, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var at1405 = time.Date(2026, 10, 21, 14, 5, 0, 0, domain.Tokyo())

func TestOpeningLabel(t *testing.T) {
	closure, err := domain.NewClosurePeriod(mustDate(t, "2026-10-20"), mustDate(t, "2026-10-25"), "特別整理期間")
	if err != nil {
		t.Fatal(err)
	}
	noReason, err := domain.NewClosurePeriod(mustDate(t, "2026-10-20"), mustDate(t, "2026-10-25"), "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   domain.Opening
		want string
	}{
		{"開館中", domain.Opening{Status: domain.Open, Hours: mustHours(t, "09:00", "19:00")}, "開館中 〜19:00"},
		{"閉館まで30分未満", domain.Opening{Status: domain.Open, Hours: mustHours(t, "09:00", "14:30"), ClosingSoon: true}, "開館中 〜14:30（閉館まで30分未満）"},
		{"開館前", domain.Opening{Status: domain.NotYetOpen, Hours: mustHours(t, "15:00", "20:00")}, "開館前 15:00〜20:00"},
		{"終了", domain.Opening{Status: domain.FinishedToday, Hours: mustHours(t, "09:00", "13:00")}, "本日の開館は終了"},
		{"定休日", domain.Opening{Status: domain.ClosedRegular}, "休館（定休日）"},
		{"臨時休館", domain.Opening{Status: domain.ClosedPeriod, Closure: closure}, "休館（特別整理期間 2026-10-20〜2026-10-25）"},
		{"臨時休館（理由なし）", domain.Opening{Status: domain.ClosedPeriod, Closure: noReason}, "休館（臨時休館 2026-10-20〜2026-10-25）"},
		{"祝日判定不可", domain.Opening{Status: domain.HolidayUnknown}, "開館情報不明（祝日判定不可）"},
		{"未入力", domain.Opening{Status: domain.NoSchedule}, "開館情報未入力"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OpeningLabel(tt.in); got != tt.want {
				t.Errorf("OpeningLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAvailabilityLabel(t *testing.T) {
	book := mustBook(t, "9784003101018", "吾輩は猫である")
	fetched := time.Date(2026, 10, 21, 13, 0, 0, 0, domain.Tokyo())
	tests := []struct {
		name string
		in   domain.BookHolding
		want string
	}{
		{"貸出可", domain.BookHolding{Availability: domain.AvailableToLend, Raw: "貸出可", SystemStatus: domain.SystemOK}, "在架（貸出可）"},
		{"館内のみ", domain.BookHolding{Availability: domain.InLibraryOnly, Raw: "館内のみ", SystemStatus: domain.SystemCache}, "在架（館内閲覧のみ）"},
		{"貸出中", domain.BookHolding{Availability: domain.OnLoan, Raw: "貸出中", SystemStatus: domain.SystemOK}, "貸出中"},
		{"予約中はそのまま", domain.BookHolding{Availability: domain.Reserved, Raw: "予約中", SystemStatus: domain.SystemOK}, "予約中"},
		{"所蔵なし", domain.BookHolding{Availability: domain.NotHeld, SystemStatus: domain.SystemOK}, "所蔵なし"},
		{"蔵書あり", domain.BookHolding{Availability: domain.Unknown, Raw: "蔵書あり", SystemStatus: domain.SystemOK}, "不明（蔵書あり／貸出状況不明）"},
		{"その他の値", domain.BookHolding{Availability: domain.Unknown, Raw: "休館中", SystemStatus: domain.SystemOK}, "不明（休館中）"},
		{"古い情報", domain.BookHolding{Availability: domain.AvailableToLend, Stale: true, SystemStatus: domain.SystemOK, FetchedAt: fetched}, "不明（古い情報: 13:00 取得）"},
		{"タイムアウト", domain.BookHolding{Availability: domain.Unknown, SystemStatus: domain.SystemRunning}, "不明（取得タイムアウト）"},
		{"取得失敗", domain.BookHolding{Availability: domain.Unknown, SystemStatus: domain.SystemError}, "不明（取得失敗）"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.Book = book
			if got := AvailabilityLabel(tt.in); got != tt.want {
				t.Errorf("AvailabilityLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHoldingsSourceLabel(t *testing.T) {
	book := mustBook(t, "9784003101018", "吾輩は猫である")
	fresh := func(at time.Time) domain.BookHolding {
		return domain.BookHolding{Book: book, Availability: domain.AvailableToLend, SystemStatus: domain.SystemOK, FetchedAt: at}
	}
	failed := domain.BookHolding{Book: book, Availability: domain.Unknown, SystemStatus: domain.SystemError, FetchedAt: at1405}
	tests := []struct {
		name string
		in   domain.Result
		want string
	}{
		{
			"取得時刻が1つ",
			domain.Result{Readable: []domain.LibraryResult{{Books: []domain.BookHolding{fresh(at1405.Add(-10 * time.Second))}}}, NotReadable: []domain.LibraryResult{{Books: []domain.BookHolding{failed}}}},
			"所蔵情報: カーリル 14:04 取得（30分以内の取得結果を使用）",
		},
		{
			"取得時刻が複数",
			domain.Result{Readable: []domain.LibraryResult{{Books: []domain.BookHolding{fresh(at1405)}}, {Books: []domain.BookHolding{fresh(at1405.Add(-15 * time.Minute))}}}},
			"所蔵情報: カーリル（30分以内の取得結果を使用。取得時刻は本ごとに表示）",
		},
		{
			"取得できなかった",
			domain.Result{NotReadable: []domain.LibraryResult{{Books: []domain.BookHolding{failed}}}},
			"所蔵情報: カーリル（最新の所蔵情報を取得できませんでした）",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HoldingsSourceLabel(tt.in); got != tt.want {
				t.Errorf("HoldingsSourceLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpeningInfoSourceLabel(t *testing.T) {
	s1 := &domain.Schedule{UpdatedOn: mustDate(t, "2026-10-01")}
	s2 := &domain.Schedule{UpdatedOn: mustDate(t, "2026-10-03")}
	libs := []domain.RegisteredLibrary{
		{ID: domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "中央図書館"}, Schedule: s1},
		{ID: domain.LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "経堂図書館"}},
		{ID: domain.LibraryID{SystemID: "Tokyo_Meguro", LibKey: "八雲中央図書館"}, Schedule: s2},
	}
	if got, want := OpeningInfoSourceLabel(libs), "開館情報: 利用者入力（最終更新 中央図書館 2026-10-01 / 八雲中央図書館 2026-10-03）"; got != want {
		t.Errorf("OpeningInfoSourceLabel = %q, want %q", got, want)
	}
	if got, want := OpeningInfoSourceLabel(libs[1:2]), "開館情報: 利用者入力"; got != want {
		t.Errorf("未入力だけ: OpeningInfoSourceLabel = %q, want %q", got, want)
	}
}

func TestCredits(t *testing.T) {
	tests := map[string][2]string{
		"ClosureNote":      {ClosureNote, "※ 臨時休館は入力済みの情報のみ反映しています"},
		"NDLCreditLabel":   {NDLCreditLabel, "書誌情報: 国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータを利用しています"},
		"NDLURL":           {NDLURL, "https://ndlsearch.ndl.go.jp/"},
		"NDLLicenseLabel":  {NDLLicenseLabel, "クリエイティブ・コモンズ 表示 4.0 国際"},
		"NDLLicenseURL":    {NDLLicenseURL, "https://creativecommons.org/licenses/by/4.0/legalcode.ja"},
		"CalilCreditLabel": {CalilCreditLabel, "図書館情報: カーリル"},
		"CalilURL":         {CalilURL, "https://calil.jp/"},
		"HolidaysLabel":    {HolidaysLabel, "祝日: holidays-jp（https://holidays-jp.github.io/）"},
	}
	for name, tt := range tests {
		if tt[0] != tt[1] {
			t.Errorf("%s = %q, want %q", name, tt[0], tt[1])
		}
	}
	if got, want := CalilBookURL(mustBook(t, "9784003101018", "x").ISBN), "https://calil.jp/book/9784003101018"; got != want {
		t.Errorf("CalilBookURL = %q, want %q", got, want)
	}
}
