package server

import (
	"net/http"
	"slices"

	"libread/internal/domain"
)

// 利用図書館（contracts/http-api.md「利用図書館」）。

type librariesResponse struct {
	Libraries []libraryJSON `json:"libraries"`
}

type libraryJSON struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Pref     string        `json:"pref"`
	City     string        `json:"city"`
	Schedule *scheduleJSON `json:"schedule"` // 未入力なら null
	Closures []closureJSON `json:"closures"` // 開始日の順
}

type scheduleJSON struct {
	Weekly    weeklyJSON  `json:"weekly"`
	Holiday   holidayJSON `json:"holiday"`
	UpdatedOn string      `json:"updatedOn"`
}

// weeklyJSON は7曜日すべてのキーを持つ。休館の曜日は null。
type weeklyJSON struct {
	Sun *hoursJSON `json:"sun"`
	Mon *hoursJSON `json:"mon"`
	Tue *hoursJSON `json:"tue"`
	Wed *hoursJSON `json:"wed"`
	Thu *hoursJSON `json:"thu"`
	Fri *hoursJSON `json:"fri"`
	Sat *hoursJSON `json:"sat"`
}

type hoursJSON struct {
	Open  string `json:"open"`
	Close string `json:"close"`
}

// holidayJSON の open・close は mode が hours のときだけ付ける。
type holidayJSON struct {
	Mode  string `json:"mode"` // same / closed / hours
	Open  string `json:"open,omitempty"`
	Close string `json:"close,omitempty"`
}

type closureJSON struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Reason string `json:"reason"`
}

func libraryJSONOf(l domain.RegisteredLibrary) libraryJSON {
	out := libraryJSON{
		ID:       l.ID.String(),
		Name:     l.Name,
		Pref:     l.Pref,
		City:     l.City,
		Closures: make([]closureJSON, 0, len(l.Closures)),
	}
	if l.Schedule != nil {
		out.Schedule = scheduleJSONOf(*l.Schedule)
	}
	closures := slices.Clone(l.Closures)
	domain.SortClosures(closures)
	for _, c := range closures {
		out.Closures = append(out.Closures, closureJSON{Start: c.Start().String(), End: c.End().String(), Reason: c.Reason()})
	}
	return out
}

func scheduleJSONOf(s domain.Schedule) *scheduleJSON {
	day := func(d domain.DayRule) *hoursJSON {
		h, open := d.Hours()
		if !open {
			return nil
		}
		return &hoursJSON{Open: h.Open().String(), Close: h.Close().String()}
	}
	w := s.Weekly
	out := &scheduleJSON{
		Weekly: weeklyJSON{
			Sun: day(w[0]), Mon: day(w[1]), Tue: day(w[2]), Wed: day(w[3]),
			Thu: day(w[4]), Fri: day(w[5]), Sat: day(w[6]),
		},
		UpdatedOn: s.UpdatedOn.String(),
	}
	switch s.Holiday.Mode() {
	case domain.HolidayClosed:
		out.Holiday = holidayJSON{Mode: "closed"}
	case domain.HolidayCustom:
		h, _ := s.Holiday.CustomHours()
		out.Holiday = holidayJSON{Mode: "hours", Open: h.Open().String(), Close: h.Close().String()}
	default:
		out.Holiday = holidayJSON{Mode: "same"}
	}
	return out
}

// handleListLibraries は利用図書館を登録順に返す。
func (s *server) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	data, err := s.loadData()
	if err != nil {
		s.logger.Printf("利用者データを読めません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	out := librariesResponse{Libraries: make([]libraryJSON, 0, len(data.Libraries))}
	for _, l := range data.Libraries {
		out.Libraries = append(out.Libraries, libraryJSONOf(l))
	}
	writeJSON(w, http.StatusOK, out)
}
