package domain

import (
	"testing"
	"time"
)

func mustHours(t *testing.T, open, close string) Hours {
	t.Helper()
	o, err := ParseTimeOfDay(open)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseTimeOfDay(close)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHours(o, c)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustClosure(t *testing.T, start, end, reason string) ClosurePeriod {
	t.Helper()
	c, err := NewClosurePeriod(mustDate(t, start), mustDate(t, end), reason)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// at は 2026-10-21（水）の日本時間 hh:mm:ss。
func at(hh, mm, ss int) time.Time {
	return time.Date(2026, 10, 21, hh, mm, ss, 0, Tokyo())
}

func notHoliday(Date) (bool, bool)     { return false, true }
func isHolidayToday(Date) (bool, bool) { return true, true }
func holidayUnknown(Date) (bool, bool) { return false, false }

func TestOpenStatusOf(t *testing.T) {
	weekday := mustHours(t, "09:00", "19:00")
	holidayHours := mustHours(t, "10:00", "17:00")

	schedule := func(holiday HolidayRule) *Schedule {
		var s Schedule
		for d := time.Sunday; d <= time.Saturday; d++ {
			s.Weekly[d] = OpenDay(weekday)
		}
		s.Weekly[time.Monday] = DayRule{} // 月曜休館
		s.Holiday = holiday
		s.UpdatedOn = mustDate(t, "2026-10-01")
		return &s
	}
	wednesdayClosed := schedule(HolidaySameAsWeekdayRule())
	wednesdayClosed.Weekly[time.Wednesday] = DayRule{}

	tests := []struct {
		name        string
		schedule    *Schedule
		closures    []ClosurePeriod
		now         time.Time
		isHoliday   func(Date) (bool, bool)
		want        OpenStatus
		wantHours   Hours
		wantClosure ClosurePeriod
		closingSoon bool
	}{
		{name: "スケジュール未入力", schedule: nil, now: at(12, 0, 0), isHoliday: notHoliday, want: NoSchedule},
		{
			name:     "臨時休館期間の初日",
			schedule: schedule(HolidaySameAsWeekdayRule()), now: at(12, 0, 0), isHoliday: notHoliday,
			closures:    []ClosurePeriod{mustClosure(t, "2026-10-21", "2026-10-24", "特別整理期間")},
			want:        ClosedPeriod,
			wantClosure: mustClosure(t, "2026-10-21", "2026-10-24", "特別整理期間"),
		},
		{
			name:     "臨時休館期間の最終日",
			schedule: schedule(HolidaySameAsWeekdayRule()), now: at(12, 0, 0), isHoliday: notHoliday,
			closures:    []ClosurePeriod{mustClosure(t, "2026-11-01", "2026-11-02", ""), mustClosure(t, "2026-10-18", "2026-10-21", "")},
			want:        ClosedPeriod,
			wantClosure: mustClosure(t, "2026-10-18", "2026-10-21", ""),
		},
		{
			name:     "臨時休館期間の前日は開館",
			schedule: schedule(HolidaySameAsWeekdayRule()), now: at(12, 0, 0), isHoliday: notHoliday,
			closures: []ClosurePeriod{mustClosure(t, "2026-10-22", "2026-10-24", "")},
			want:     Open, wantHours: weekday,
		},
		{
			name:     "臨時休館期間の翌日は開館",
			schedule: schedule(HolidaySameAsWeekdayRule()), now: at(12, 0, 0), isHoliday: notHoliday,
			closures: []ClosurePeriod{mustClosure(t, "2026-10-15", "2026-10-20", "")},
			want:     Open, wantHours: weekday,
		},
		{
			name:     "臨時休館は祝日判定より優先",
			schedule: schedule(HolidayClosedRule()), now: at(12, 0, 0), isHoliday: holidayUnknown,
			closures:    []ClosurePeriod{mustClosure(t, "2026-10-21", "2026-10-21", "")},
			want:        ClosedPeriod,
			wantClosure: mustClosure(t, "2026-10-21", "2026-10-21", ""),
		},
		{name: "祝日判定不可・祝日休館", schedule: schedule(HolidayClosedRule()), now: at(12, 0, 0), isHoliday: holidayUnknown, want: HolidayUnknown},
		{name: "祝日判定不可・祝日は独自の時間", schedule: schedule(HolidayCustomHours(holidayHours)), now: at(12, 0, 0), isHoliday: holidayUnknown, want: HolidayUnknown},
		{name: "祝日判定不可でも曜日どおりなら判定できる", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(12, 0, 0), isHoliday: holidayUnknown, want: Open, wantHours: weekday},
		{name: "祝日・祝日休館", schedule: schedule(HolidayClosedRule()), now: at(12, 0, 0), isHoliday: isHolidayToday, want: ClosedRegular},
		{name: "祝日・独自の時間で開館中", schedule: schedule(HolidayCustomHours(holidayHours)), now: at(12, 0, 0), isHoliday: isHolidayToday, want: Open, wantHours: holidayHours},
		{name: "祝日・独自の時間で終了", schedule: schedule(HolidayCustomHours(holidayHours)), now: at(17, 0, 0), isHoliday: isHolidayToday, want: FinishedToday, wantHours: holidayHours},
		{name: "祝日・曜日どおり", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(12, 0, 0), isHoliday: isHolidayToday, want: Open, wantHours: weekday},
		{name: "祝日でない日は祝日休館の館も曜日どおり", schedule: schedule(HolidayClosedRule()), now: at(12, 0, 0), isHoliday: notHoliday, want: Open, wantHours: weekday},
		{name: "定休日（曜日）", schedule: wednesdayClosed, now: at(12, 0, 0), isHoliday: notHoliday, want: ClosedRegular},
		{name: "開館前", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(8, 59, 59), isHoliday: notHoliday, want: NotYetOpen, wantHours: weekday},
		{name: "ちょうど開館時刻", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(9, 0, 0), isHoliday: notHoliday, want: Open, wantHours: weekday},
		{name: "閉館まで30分ちょうど", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(18, 30, 0), isHoliday: notHoliday, want: Open, wantHours: weekday},
		{name: "閉館まで30分未満", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(18, 30, 1), isHoliday: notHoliday, want: Open, wantHours: weekday, closingSoon: true},
		{name: "閉館直前", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(18, 59, 59), isHoliday: notHoliday, want: Open, wantHours: weekday, closingSoon: true},
		{name: "ちょうど閉館時刻", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(19, 0, 0), isHoliday: notHoliday, want: FinishedToday, wantHours: weekday},
		{name: "閉館後", schedule: schedule(HolidaySameAsWeekdayRule()), now: at(22, 0, 0), isHoliday: notHoliday, want: FinishedToday, wantHours: weekday},
		{
			name: "UTC の時刻も日本時間で判定する", schedule: schedule(HolidaySameAsWeekdayRule()),
			now: time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC), isHoliday: notHoliday, want: Open, wantHours: weekday,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lib := RegisteredLibrary{
				ID:       LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "中央"},
				Name:     "世田谷区立中央図書館",
				Schedule: tt.schedule,
				Closures: tt.closures,
			}
			got := OpenStatusOf(lib, tt.now, tt.isHoliday)
			if got.Status != tt.want {
				t.Errorf("Status = %v, want %v", got.Status, tt.want)
			}
			if got.Hours != tt.wantHours {
				t.Errorf("Hours = %v, want %v", got.Hours, tt.wantHours)
			}
			if got.Closure != tt.wantClosure {
				t.Errorf("Closure = %+v, want %+v", got.Closure, tt.wantClosure)
			}
			if got.ClosingSoon != tt.closingSoon {
				t.Errorf("ClosingSoon = %v, want %v", got.ClosingSoon, tt.closingSoon)
			}
		})
	}
}

func TestOpenStatusOfAsksHolidayForToday(t *testing.T) {
	var s Schedule
	s.Weekly[time.Wednesday] = OpenDay(mustHours(t, "09:00", "19:00"))
	s.Holiday = HolidayClosedRule()
	lib := RegisteredLibrary{ID: LibraryID{SystemID: "S", LibKey: "K"}, Schedule: &s}

	var asked []Date
	OpenStatusOf(lib, at(12, 0, 0), func(d Date) (bool, bool) {
		asked = append(asked, d)
		return false, true
	})
	if len(asked) != 1 || asked[0] != mustDate(t, "2026-10-21") {
		t.Errorf("isHoliday に渡された日付 = %v, want [2026-10-21]", asked)
	}
}
