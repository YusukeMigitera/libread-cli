package domain

import "time"

// OpenStatus は利用図書館の今日の開館状態。
type OpenStatus int

const (
	NoSchedule     OpenStatus = iota // 開館スケジュールが未入力
	Open                             // 開館中
	NotYetOpen                       // 今日開館するが、まだ開館時刻前
	FinishedToday                    // 今日の閉館時刻を過ぎた
	ClosedRegular                    // 定休日（曜日、または祝日の扱いが「休館」）
	ClosedPeriod                     // 臨時休館期間に今日が含まれる
	HolidayUnknown                   // 祝日か判定できず、判定によって結果が変わる
)

func (s OpenStatus) String() string {
	switch s {
	case Open:
		return "Open"
	case NotYetOpen:
		return "NotYetOpen"
	case FinishedToday:
		return "FinishedToday"
	case ClosedRegular:
		return "ClosedRegular"
	case ClosedPeriod:
		return "ClosedPeriod"
	case HolidayUnknown:
		return "HolidayUnknown"
	default:
		return "NoSchedule"
	}
}

// closingSoonWithin は「閉館まで30分未満」と注記する残り時間（spec Edge Cases）。
const closingSoonWithin = 30 * time.Minute

// Opening は OpenStatusOf の結果。
type Opening struct {
	Status OpenStatus
	// Hours は今日の開館時間。Status が Open・NotYetOpen・FinishedToday のときだけ意味を持つ。
	Hours Hours
	// Closure は今日を含む臨時休館期間。Status が ClosedPeriod のときだけ意味を持つ。
	Closure ClosurePeriod
	// ClosingSoon は開館中で、閉館まで30分未満なら true。
	ClosingSoon bool
}

// OpenStatusOf は now の時点での lib の開館状態を data-model.md「OpenStatusOf の判定順」に従って求める。
// isHoliday は日付が祝日か（isHoliday）と、それを判定できたか（known）を返す。
func OpenStatusOf(lib RegisteredLibrary, now time.Time, isHoliday func(Date) (holiday, known bool)) Opening {
	if lib.Schedule == nil {
		return Opening{Status: NoSchedule}
	}
	today := DateOf(now)
	for _, c := range lib.Closures {
		if c.Contains(today) {
			return Opening{Status: ClosedPeriod, Closure: c}
		}
	}

	rule := lib.Schedule.Weekly[today.Weekday()]
	if lib.Schedule.Holiday.Mode() != HolidaySameAsWeekday {
		holiday, known := isHoliday(today)
		if !known {
			return Opening{Status: HolidayUnknown}
		}
		if holiday {
			if h, ok := lib.Schedule.Holiday.CustomHours(); ok {
				rule = OpenDay(h)
			} else {
				rule = DayRule{}
			}
		}
	}

	hours, open := rule.Hours()
	if !open {
		return Opening{Status: ClosedRegular}
	}
	local := now.In(tokyo)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, tokyo)
	openAt := midnight.Add(time.Duration(hours.Open()) * time.Minute)
	closeAt := midnight.Add(time.Duration(hours.Close()) * time.Minute)
	switch {
	case now.Before(openAt):
		return Opening{Status: NotYetOpen, Hours: hours}
	case now.Before(closeAt):
		return Opening{Status: Open, Hours: hours, ClosingSoon: closeAt.Sub(now) < closingSoonWithin}
	default:
		return Opening{Status: FinishedToday, Hours: hours}
	}
}
