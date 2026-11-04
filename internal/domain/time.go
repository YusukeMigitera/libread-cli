package domain

import (
	"fmt"
	"time"
	_ "time/tzdata" // どの環境でも Asia/Tokyo を読めるようにする（research.md R7）
)

var tokyo = mustLoadLocation("Asia/Tokyo")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("タイムゾーン %s を読み込めません: %v", name, err))
	}
	return loc
}

// Tokyo は libread が「今日」を判断するタイムゾーン（spec Assumptions）。
func Tokyo() *time.Location { return tokyo }

// TimeOfDay は 0:00 からの分数。00:00〜24:00。
type TimeOfDay int

const maxTimeOfDay TimeOfDay = 24 * 60

// ParseTimeOfDay は "HH:MM" 形式を読む。
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	if len(s) != 5 || s[2] != ':' || !allDigits(s[:2]) || !allDigits(s[3:]) {
		return 0, fmt.Errorf("時刻 %q: HH:MM 形式で指定してください", s)
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	t := TimeOfDay(h*60 + m)
	if m >= 60 || t > maxTimeOfDay {
		return 0, fmt.Errorf("時刻 %q: 00:00〜24:00 の範囲で指定してください", s)
	}
	return t, nil
}

func (t TimeOfDay) String() string {
	return fmt.Sprintf("%02d:%02d", int(t)/60, int(t)%60)
}

// Hours は開館時刻と閉館時刻の組。開館 < 閉館。
type Hours struct {
	open, close TimeOfDay
}

func NewHours(open, close TimeOfDay) (Hours, error) {
	if open >= close {
		return Hours{}, fmt.Errorf("開館時刻 %s は閉館時刻 %s より前にしてください", open, close)
	}
	return Hours{open: open, close: close}, nil
}

func (h Hours) Open() TimeOfDay  { return h.open }
func (h Hours) Close() TimeOfDay { return h.close }

func (h Hours) String() string { return h.open.String() + "-" + h.close.String() }

// Date は Asia/Tokyo の暦日。
type Date struct {
	year  int
	month time.Month
	day   int
}

const dateLayout = "2006-01-02"

// ParseDate は "YYYY-MM-DD" 形式を読む。
func ParseDate(s string) (Date, error) {
	t, err := time.ParseInLocation(dateLayout, s, tokyo)
	if err != nil {
		return Date{}, fmt.Errorf("日付 %q: YYYY-MM-DD 形式で指定してください", s)
	}
	return DateOf(t), nil
}

// DateOf は t を Asia/Tokyo に変換した日付を返す。
func DateOf(t time.Time) Date {
	y, m, d := t.In(tokyo).Date()
	return Date{year: y, month: m, day: d}
}

func (d Date) String() string { return d.start().Format(dateLayout) }

func (d Date) Before(o Date) bool { return d.start().Before(o.start()) }

// Compare は d が o より前なら -1、同じ日なら 0、後なら 1 を返す。
func (d Date) Compare(o Date) int { return d.start().Compare(o.start()) }
func (d Date) After(o Date) bool  { return d.start().After(o.start()) }

// Weekday はその日の曜日。
func (d Date) Weekday() time.Weekday { return d.start().Weekday() }

func (d Date) start() time.Time { return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, tokyo) }

// Year はその日の年。
func (d Date) Year() int { return d.year }
