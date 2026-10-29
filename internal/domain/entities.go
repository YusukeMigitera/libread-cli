package domain

import (
	"errors"
	"fmt"
	"slices"
)

// Book は書誌情報。ISBN で同一性を判断する。
type Book struct {
	ISBN      ISBN
	Title     string
	Author    string
	Publisher string
}

// NewBook は書名が空の Book を作らない。
func NewBook(isbn ISBN, title, author, publisher string) (Book, error) {
	if title == "" {
		return Book{}, fmt.Errorf("ISBN %s: 書名がありません", isbn)
	}
	return Book{ISBN: isbn, Title: title, Author: author, Publisher: publisher}, nil
}

// ReadingBook は読みかけの本。Note は外部サービスへ送らない。
type ReadingBook struct {
	Book    Book
	Note    string
	AddedOn Date
}

// RegisteredLibrary は利用図書館。開館スケジュールが未入力なら Schedule は nil。
type RegisteredLibrary struct {
	ID       LibraryID
	Name     string
	Pref     string
	City     string
	Schedule *Schedule
	Closures []ClosurePeriod
}

// Schedule は利用者が入力した開館スケジュール。Weekly は time.Weekday（日曜 = 0）で引く。
type Schedule struct {
	Weekly    [7]DayRule
	Holiday   HolidayRule
	UpdatedOn Date
}

// DayRule はある曜日の開館時間。ゼロ値は休館。
type DayRule struct {
	hours Hours
	open  bool
}

func OpenDay(h Hours) DayRule { return DayRule{hours: h, open: true} }

// Hours は開館する日なら開館時間と true を返す。
func (d DayRule) Hours() (Hours, bool) { return d.hours, d.open }

// HolidayMode は祝日の扱い。
type HolidayMode int

const (
	HolidaySameAsWeekday HolidayMode = iota
	HolidayClosed
	HolidayCustom
)

// HolidayRule は祝日の扱い。ゼロ値は「曜日どおり」。
type HolidayRule struct {
	mode  HolidayMode
	hours Hours
}

func HolidaySameAsWeekdayRule() HolidayRule      { return HolidayRule{mode: HolidaySameAsWeekday} }
func HolidayClosedRule() HolidayRule             { return HolidayRule{mode: HolidayClosed} }
func HolidayCustomHours(h Hours) HolidayRule     { return HolidayRule{mode: HolidayCustom, hours: h} }
func (r HolidayRule) Mode() HolidayMode          { return r.mode }
func (r HolidayRule) CustomHours() (Hours, bool) { return r.hours, r.mode == HolidayCustom }

// ClosurePeriod は臨時休館・特別整理期間。両端の日を含む。
type ClosurePeriod struct {
	start, end Date
	reason     string
}

func NewClosurePeriod(start, end Date, reason string) (ClosurePeriod, error) {
	if end.Before(start) {
		return ClosurePeriod{}, errors.New("臨時休館期間の開始日は終了日以前にしてください")
	}
	return ClosurePeriod{start: start, end: end, reason: reason}, nil
}

func (c ClosurePeriod) Start() Date    { return c.start }
func (c ClosurePeriod) End() Date      { return c.end }
func (c ClosurePeriod) Reason() string { return c.reason }

// SortClosures は期間を開始日の順に並べる。開始日が同じなら元の順のまま。
func SortClosures(cs []ClosurePeriod) {
	slices.SortStableFunc(cs, func(a, b ClosurePeriod) int { return a.start.Compare(b.start) })
}

// Contains は d が期間内（両端を含む）かを返す。
func (c ClosurePeriod) Contains(d Date) bool { return !d.Before(c.start) && !d.After(c.end) }
