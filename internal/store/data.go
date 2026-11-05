// Package store は利用者データ（data.json）とキャッシュをファイルに読み書きする。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"libread/internal/domain"
)

const (
	dataFileName  = "data.json"
	schemaVersion = 1
)

// Data は利用者データのすべて。
type Data struct {
	Books     []domain.ReadingBook
	Libraries []domain.RegisteredLibrary
}

// ResolveDataDir は LIBREAD_DATA_DIR の値（空なら os.UserConfigDir()/libread）を返す。
func ResolveDataDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("設定ディレクトリを特定できません（LIBREAD_DATA_DIR で指定できます）: %w", err)
	}
	return filepath.Join(base, "libread"), nil
}

// Load は dir/data.json を読む。ファイルがなければ空のデータを返す。
func Load(dir string) (Data, error) {
	path := filepath.Join(dir, dataFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Data{}, nil
	}
	if err != nil {
		return Data{}, fmt.Errorf("%s を読み込めません: %w", path, err)
	}
	d, err := ParseData(raw)
	if err != nil {
		return Data{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// ParseData は data.json の中身を検証して読む。001 の data.json の取り込み（research W8）でも使う。
func ParseData(raw []byte) (Data, error) {
	var f dataFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Data{}, fmt.Errorf("形式が正しくありません: %w", err)
	}
	if f.Version != schemaVersion {
		return Data{}, fmt.Errorf("対応していないバージョン %d です", f.Version)
	}
	return f.toDomain()
}

// Save は d を dir/data.json にアトミックに書き込む。
func Save(dir string, d Data) error {
	raw, err := json.MarshalIndent(fromDomain(d), "", "  ")
	if err != nil {
		return fmt.Errorf("利用者データを変換できません: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, dataFileName), append(raw, '\n'))
}

// 以下は data.json の形式（data-model.md「永続化形式」）。ドメインの型とはこの境界で変換する。

type dataFile struct {
	Version   int           `json:"version"`
	Books     []bookJSON    `json:"books"`
	Libraries []libraryJSON `json:"libraries"`
}

type bookJSON struct {
	ISBN      string `json:"isbn"`
	Title     string `json:"title"`
	Author    string `json:"author,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Note      string `json:"note,omitempty"`
	AddedOn   string `json:"addedOn"`
}

type libraryJSON struct {
	SystemID string        `json:"systemId"`
	LibKey   string        `json:"libKey"`
	Name     string        `json:"name"`
	Pref     string        `json:"pref,omitempty"`
	City     string        `json:"city,omitempty"`
	Schedule *scheduleJSON `json:"schedule,omitempty"`
	Closures []closureJSON `json:"closures"`
}

type scheduleJSON struct {
	Weekly    map[string]dayJSON `json:"weekly"`
	Holiday   holidayJSON        `json:"holiday"`
	UpdatedOn string             `json:"updatedOn"`
}

type dayJSON struct {
	Closed bool   `json:"closed,omitempty"`
	Open   string `json:"open,omitempty"`
	Close  string `json:"close,omitempty"`
}

type holidayJSON struct {
	Mode  string `json:"mode"`
	Open  string `json:"open,omitempty"`
	Close string `json:"close,omitempty"`
}

type closureJSON struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Reason string `json:"reason,omitempty"`
}

var weekdayKeys = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

const (
	holidayModeSame   = "same"
	holidayModeClosed = "closed"
	holidayModeHours  = "hours"
)

func (f dataFile) toDomain() (Data, error) {
	d := Data{
		Books:     make([]domain.ReadingBook, 0, len(f.Books)),
		Libraries: make([]domain.RegisteredLibrary, 0, len(f.Libraries)),
	}
	for _, b := range f.Books {
		rb, err := b.toDomain()
		if err != nil {
			return Data{}, err
		}
		d.Books = append(d.Books, rb)
	}
	for _, l := range f.Libraries {
		rl, err := l.toDomain()
		if err != nil {
			return Data{}, err
		}
		d.Libraries = append(d.Libraries, rl)
	}
	return d, nil
}

func (b bookJSON) toDomain() (domain.ReadingBook, error) {
	isbn, err := domain.ParseISBN(b.ISBN)
	if err != nil {
		return domain.ReadingBook{}, err
	}
	book, err := domain.NewBook(isbn, b.Title, b.Author, b.Publisher)
	if err != nil {
		return domain.ReadingBook{}, err
	}
	added, err := domain.ParseDate(b.AddedOn)
	if err != nil {
		return domain.ReadingBook{}, fmt.Errorf("本 %s の登録日: %w", isbn, err)
	}
	return domain.ReadingBook{Book: book, Note: b.Note, AddedOn: added}, nil
}

func (l libraryJSON) toDomain() (domain.RegisteredLibrary, error) {
	id := domain.LibraryID{SystemID: l.SystemID, LibKey: l.LibKey}
	if _, err := domain.ParseLibraryID(id.String()); err != nil {
		return domain.RegisteredLibrary{}, err
	}
	lib := domain.RegisteredLibrary{
		ID:       id,
		Name:     l.Name,
		Pref:     l.Pref,
		City:     l.City,
		Closures: make([]domain.ClosurePeriod, 0, len(l.Closures)),
	}
	if l.Schedule != nil {
		s, err := l.Schedule.toDomain()
		if err != nil {
			return domain.RegisteredLibrary{}, fmt.Errorf("図書館 %s の開館スケジュール: %w", id, err)
		}
		lib.Schedule = &s
	}
	for _, c := range l.Closures {
		cp, err := c.toDomain()
		if err != nil {
			return domain.RegisteredLibrary{}, fmt.Errorf("図書館 %s の臨時休館期間: %w", id, err)
		}
		lib.Closures = append(lib.Closures, cp)
	}
	domain.SortClosures(lib.Closures) // 手で編集されても、番号（lib closure list）が開始日の順になるようにする
	return lib, nil
}

func (s scheduleJSON) toDomain() (domain.Schedule, error) {
	var out domain.Schedule
	for key, day := range s.Weekly {
		wd, ok := weekdayOf(key)
		if !ok {
			return domain.Schedule{}, fmt.Errorf("曜日 %q は sun〜sat で指定してください", key)
		}
		if day.Closed {
			continue // ゼロ値の DayRule が休館
		}
		h, err := parseHours(day.Open, day.Close)
		if err != nil {
			return domain.Schedule{}, fmt.Errorf("%s: %w", key, err)
		}
		out.Weekly[wd] = domain.OpenDay(h)
	}
	switch s.Holiday.Mode {
	case holidayModeSame:
		out.Holiday = domain.HolidaySameAsWeekdayRule()
	case holidayModeClosed:
		out.Holiday = domain.HolidayClosedRule()
	case holidayModeHours:
		h, err := parseHours(s.Holiday.Open, s.Holiday.Close)
		if err != nil {
			return domain.Schedule{}, fmt.Errorf("祝日: %w", err)
		}
		out.Holiday = domain.HolidayCustomHours(h)
	default:
		return domain.Schedule{}, fmt.Errorf("祝日の扱い %q は same / closed / hours で指定してください", s.Holiday.Mode)
	}
	updated, err := domain.ParseDate(s.UpdatedOn)
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("最終更新日: %w", err)
	}
	out.UpdatedOn = updated
	return out, nil
}

func (c closureJSON) toDomain() (domain.ClosurePeriod, error) {
	start, err := domain.ParseDate(c.Start)
	if err != nil {
		return domain.ClosurePeriod{}, err
	}
	end, err := domain.ParseDate(c.End)
	if err != nil {
		return domain.ClosurePeriod{}, err
	}
	return domain.NewClosurePeriod(start, end, c.Reason)
}

func weekdayOf(key string) (time.Weekday, bool) {
	for i, k := range weekdayKeys {
		if k == key {
			return time.Weekday(i), true
		}
	}
	return 0, false
}

func parseHours(open, close string) (domain.Hours, error) {
	o, err := domain.ParseTimeOfDay(open)
	if err != nil {
		return domain.Hours{}, err
	}
	c, err := domain.ParseTimeOfDay(close)
	if err != nil {
		return domain.Hours{}, err
	}
	return domain.NewHours(o, c)
}

func fromDomain(d Data) dataFile {
	f := dataFile{
		Version:   schemaVersion,
		Books:     make([]bookJSON, 0, len(d.Books)),
		Libraries: make([]libraryJSON, 0, len(d.Libraries)),
	}
	for _, b := range d.Books {
		f.Books = append(f.Books, bookJSON{
			ISBN:      b.Book.ISBN.String(),
			Title:     b.Book.Title,
			Author:    b.Book.Author,
			Publisher: b.Book.Publisher,
			Note:      b.Note,
			AddedOn:   b.AddedOn.String(),
		})
	}
	for _, l := range d.Libraries {
		lj := libraryJSON{
			SystemID: l.ID.SystemID,
			LibKey:   l.ID.LibKey,
			Name:     l.Name,
			Pref:     l.Pref,
			City:     l.City,
			Closures: make([]closureJSON, 0, len(l.Closures)),
		}
		if l.Schedule != nil {
			lj.Schedule = scheduleFromDomain(*l.Schedule)
		}
		for _, c := range l.Closures {
			lj.Closures = append(lj.Closures, closureJSON{
				Start:  c.Start().String(),
				End:    c.End().String(),
				Reason: c.Reason(),
			})
		}
		f.Libraries = append(f.Libraries, lj)
	}
	return f
}

func scheduleFromDomain(s domain.Schedule) *scheduleJSON {
	out := &scheduleJSON{
		Weekly:    make(map[string]dayJSON, len(weekdayKeys)),
		UpdatedOn: s.UpdatedOn.String(),
	}
	// 休館日も明示して書き、ファイルを見ただけで全曜日が分かるようにする。
	for i, rule := range s.Weekly {
		if h, open := rule.Hours(); open {
			out.Weekly[weekdayKeys[i]] = dayJSON{Open: h.Open().String(), Close: h.Close().String()}
		} else {
			out.Weekly[weekdayKeys[i]] = dayJSON{Closed: true}
		}
	}
	switch s.Holiday.Mode() {
	case domain.HolidaySameAsWeekday:
		out.Holiday = holidayJSON{Mode: holidayModeSame}
	case domain.HolidayClosed:
		out.Holiday = holidayJSON{Mode: holidayModeClosed}
	case domain.HolidayCustom:
		h, _ := s.Holiday.CustomHours()
		out.Holiday = holidayJSON{Mode: holidayModeHours, Open: h.Open().String(), Close: h.Close().String()}
	}
	return out
}
