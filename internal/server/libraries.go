package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"libread/internal/calil"
	"libread/internal/domain"
	"libread/internal/present"
	"libread/internal/store"
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

// findLibrary は利用図書館のうち id の館の位置。なければ -1。
func findLibrary(libs []domain.RegisteredLibrary, id domain.LibraryID) int {
	return slices.IndexFunc(libs, func(l domain.RegisteredLibrary) bool { return l.ID == id })
}

// 図書館の検索（GET /api/library-search）

type librarySearchResponse struct {
	Libraries []libraryCandidateJSON `json:"libraries"`
	Credit    labelLink              `json:"credit"`
}

type libraryCandidateJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Pref       string `json:"pref"`
	City       string `json:"city"`
	Address    string `json:"address"`
	Registered bool   `json:"registered"`
}

// calilCredit はカーリルの利用条件で必須のリンク表示（001 research R1）。
var calilCredit = labelLink{Label: present.CalilCreditLabel, URL: present.CalilURL}

// handleLibrarySearch は都道府県（必須）と市区町村でカーリルの図書館を探し、館名の部分一致で絞る（001 の lib find と同じ）。
// 館名はカーリルに送らない。
func (s *server) handleLibrarySearch(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	pref := strings.TrimSpace(params.Get("pref"))
	city := strings.TrimSpace(params.Get("city"))
	name := strings.TrimSpace(params.Get("name"))
	if pref == "" {
		writeError(w, http.StatusBadRequest, "invalid", "都道府県を選んでください")
		return
	}
	var found []calil.LibraryInfo
	err := s.withCalil(func(c *calil.Client) error {
		var err error
		found, err = c.Libraries(r.Context(), pref, city)
		return err
	})
	if err != nil {
		s.writeCalilError(w, "図書館の検索", err)
		return
	}
	// 登録済みかどうかは、問い合わせたあとのデータで決める。
	data, err := s.loadData()
	if err != nil {
		s.logger.Printf("利用者データを読めません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	out := librarySearchResponse{Libraries: []libraryCandidateJSON{}, Credit: calilCredit}
	for _, l := range found {
		if !strings.Contains(l.Formal, name) {
			continue
		}
		out.Libraries = append(out.Libraries, libraryCandidateJSON{
			ID: l.ID.String(), Name: l.Formal, Pref: l.Pref, City: l.City, Address: l.Address,
			Registered: findLibrary(data.Libraries, l.ID) >= 0,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// writeCalilError はカーリルに問い合わせられなかったことを返す。キーがなければ 503、それ以外は 502。
// ログには要求の URL（検索語を含む）を書かない（research W11）。アプリケーションキーは calil が伏せる。
func (s *server) writeCalilError(w http.ResponseWriter, what string, err error) {
	if errors.Is(err, errNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "notConfigured", "カーリルのアプリケーションキーが設定されていません")
		return
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = fmt.Errorf("カーリルに接続できません: %w", ue.Err)
	}
	s.logger.Printf("%s: %v", what, err)
	writeError(w, http.StatusBadGateway, "upstream", "カーリルに接続できませんでした。しばらくしてから再試行してください")
}

// 図書館の登録・削除

type addLibraryRequest struct {
	ID string `json:"id"`
}

// handleAddLibrary はカーリルで SystemID:LibKey の館を確かめ、利用図書館の最後に加える（001 の lib add と同じ）。
func (s *server) handleAddLibrary(w http.ResponseWriter, r *http.Request) {
	var req addLibraryRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "要求の形式が正しくありません")
		return
	}
	id, err := domain.ParseLibraryID(strings.TrimSpace(req.ID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}

	// 登録済みならカーリルに問い合わせない。
	data, err := s.loadData()
	if err != nil {
		s.logger.Printf("利用者データを読めません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	if i := findLibrary(data.Libraries, id); i >= 0 {
		writeLibraryAlreadyRegistered(w, data.Libraries[i])
		return
	}

	var found []calil.LibraryInfo
	err = s.withCalil(func(c *calil.Client) error {
		var err error
		found, err = c.LibrariesOfSystem(r.Context(), id.SystemID)
		return err
	})
	if err != nil {
		s.writeCalilError(w, "図書館の登録", err)
		return
	}
	i := slices.IndexFunc(found, func(l calil.LibraryInfo) bool { return l.ID == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "notFound", fmt.Sprintf("図書館 %s はカーリルに見つかりませんでした", id))
		return
	}
	info := found[i]
	added := domain.RegisteredLibrary{ID: id, Name: info.Formal, Pref: info.Pref, City: info.City, Closures: []domain.ClosurePeriod{}}

	var existing domain.RegisteredLibrary
	_, err = s.updateData(func(d *store.Data) error {
		// カーリルに問い合わせているあいだに、同じ館が登録されたかもしれない。
		if i := findLibrary(d.Libraries, id); i >= 0 {
			existing = d.Libraries[i]
			return errAlreadyRegistered
		}
		d.Libraries = append(d.Libraries, added)
		return nil
	})
	switch {
	case errors.Is(err, errAlreadyRegistered):
		writeLibraryAlreadyRegistered(w, existing)
	case err != nil:
		s.writeSaveError(w, "図書館の登録", err)
	default:
		writeJSON(w, http.StatusCreated, libraryJSONOf(added))
	}
}

func writeLibraryAlreadyRegistered(w http.ResponseWriter, l domain.RegisteredLibrary) {
	writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("%sはすでに利用図書館に登録されています", l.Name))
}

// handleRemoveLibrary は利用図書館から外す。
func (s *server) handleRemoveLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathLibraryID(w, r)
	if !ok {
		return
	}
	_, err := s.updateData(func(d *store.Data) error {
		i := findLibrary(d.Libraries, id)
		if i < 0 {
			return errNotRegistered
		}
		d.Libraries = slices.Delete(d.Libraries, i, i+1)
		return nil
	})
	switch {
	case errors.Is(err, errNotRegistered):
		writeLibraryNotFound(w)
	case err != nil:
		s.writeSaveError(w, "図書館の削除", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// pathLibraryID は URL の {id}（SystemID:LibKey を URL エンコードしたもの）を読む。
// 形式が違えば、登録していない館と同じく 404 を返して false。
func pathLibraryID(w http.ResponseWriter, r *http.Request) (domain.LibraryID, bool) {
	id, err := domain.ParseLibraryID(r.PathValue("id"))
	if err != nil {
		writeLibraryNotFound(w)
		return domain.LibraryID{}, false
	}
	return id, true
}

func writeLibraryNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "notFound", "この図書館は利用図書館に登録されていません")
}

// updateLibrary は利用者データの id の館に change を当てて保存し、更新後の館を返す。
// 館がなければ errNotRegistered。change がエラーを返したら保存しない。
func (s *server) updateLibrary(id domain.LibraryID, change func(*domain.RegisteredLibrary) error) (domain.RegisteredLibrary, error) {
	var updated domain.RegisteredLibrary
	_, err := s.updateData(func(d *store.Data) error {
		i := findLibrary(d.Libraries, id)
		if i < 0 {
			return errNotRegistered
		}
		if err := change(&d.Libraries[i]); err != nil {
			return err
		}
		updated = d.Libraries[i]
		return nil
	})
	return updated, err
}

// writeLibraryUpdate は updateLibrary の結果を返す。
func (s *server) writeLibraryUpdate(w http.ResponseWriter, what string, status int, l domain.RegisteredLibrary, err error) {
	switch {
	case errors.Is(err, errNotRegistered):
		writeLibraryNotFound(w)
	case errors.Is(err, errClosureExists):
		writeError(w, http.StatusConflict, "conflict", "この臨時休館期間はすでに登録されています")
	case errors.Is(err, errClosureNotFound):
		writeClosureNotFound(w)
	case err != nil:
		s.writeSaveError(w, what, err)
	default:
		writeJSON(w, status, libraryJSONOf(l))
	}
}

// 開館スケジュール（PUT /api/libraries/{id}/schedule）

type putScheduleRequest struct {
	// Weekly は7曜日すべてのキーを持つ。休館の曜日は null。
	Weekly  map[string]*hoursJSON `json:"weekly"`
	Holiday *holidayJSON          `json:"holiday"`
}

// weekdayKeys は日曜から土曜の順の曜日のキー（domain.Schedule.Weekly の順）。
var weekdayKeys = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

var weekdayLabels = [7]string{"日曜日", "月曜日", "火曜日", "水曜日", "木曜日", "金曜日", "土曜日"}

// handlePutSchedule は開館スケジュールを丸ごと置き換える。最終更新日は今日にする。
func (s *server) handlePutSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathLibraryID(w, r)
	if !ok {
		return
	}
	var req putScheduleRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "要求の形式が正しくありません")
		return
	}
	sched, err := scheduleOf(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	sched.UpdatedOn = domain.DateOf(s.cfg.Now())
	l, err := s.updateLibrary(id, func(l *domain.RegisteredLibrary) error {
		l.Schedule = &sched
		return nil
	})
	s.writeLibraryUpdate(w, "開館スケジュールの保存", http.StatusOK, l, err)
}

// scheduleOf は要求を開館スケジュールに変換する。7曜日すべてのキーがあり、時刻が正しいことを確かめる。
func scheduleOf(req putScheduleRequest) (domain.Schedule, error) {
	var sched domain.Schedule
	if req.Weekly == nil {
		return sched, errors.New("曜日ごとの開館時間（weekly）を指定してください")
	}
	if len(req.Weekly) != len(weekdayKeys) {
		return sched, errors.New("曜日ごとの開館時間は sun〜sat の7曜日すべてを指定してください")
	}
	for i, key := range weekdayKeys {
		h, ok := req.Weekly[key]
		if !ok {
			return sched, errors.New("曜日ごとの開館時間は sun〜sat の7曜日すべてを指定してください")
		}
		if h == nil {
			continue // 休館
		}
		hours, err := hoursOf(*h)
		if err != nil {
			return sched, fmt.Errorf("%s: %w", weekdayLabels[i], err)
		}
		sched.Weekly[i] = domain.OpenDay(hours)
	}
	if req.Holiday == nil {
		return sched, errors.New("祝日の扱い（holiday）を指定してください")
	}
	switch req.Holiday.Mode {
	case "same":
		sched.Holiday = domain.HolidaySameAsWeekdayRule()
	case "closed":
		sched.Holiday = domain.HolidayClosedRule()
	case "hours":
		hours, err := hoursOf(hoursJSON{Open: req.Holiday.Open, Close: req.Holiday.Close})
		if err != nil {
			return sched, fmt.Errorf("祝日: %w", err)
		}
		sched.Holiday = domain.HolidayCustomHours(hours)
	default:
		return sched, errors.New("祝日の扱いは same・closed・hours のどれかで指定してください")
	}
	return sched, nil
}

func hoursOf(h hoursJSON) (domain.Hours, error) {
	open, err := domain.ParseTimeOfDay(h.Open)
	if err != nil {
		return domain.Hours{}, err
	}
	closeAt, err := domain.ParseTimeOfDay(h.Close)
	if err != nil {
		return domain.Hours{}, err
	}
	return domain.NewHours(open, closeAt)
}

// 臨時休館期間（POST /api/libraries/{id}/closures・DELETE /api/libraries/{id}/closures/{index}）

type addClosureRequest struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Reason string `json:"reason"`
}

// handleAddClosure は臨時休館期間を加える。開始日・終了日・理由がすべて同じ期間は二重に登録しない。
// 重なるだけの期間は意図して入れることがあるので認める（001 の lib closure add と同じ）。期間は開始日の順に保存する。
func (s *server) handleAddClosure(w http.ResponseWriter, r *http.Request) {
	id, ok := pathLibraryID(w, r)
	if !ok {
		return
	}
	var req addClosureRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "要求の形式が正しくありません")
		return
	}
	period, err := closureOf(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	l, err := s.updateLibrary(id, func(l *domain.RegisteredLibrary) error {
		if slices.Contains(l.Closures, period) {
			return errClosureExists
		}
		closures := append(slices.Clone(l.Closures), period)
		domain.SortClosures(closures)
		l.Closures = closures
		return nil
	})
	s.writeLibraryUpdate(w, "臨時休館期間の追加", http.StatusCreated, l, err)
}

func closureOf(req addClosureRequest) (domain.ClosurePeriod, error) {
	start, err := domain.ParseDate(req.Start)
	if err != nil {
		return domain.ClosurePeriod{}, fmt.Errorf("開始日: %w", err)
	}
	end, err := domain.ParseDate(req.End)
	if err != nil {
		return domain.ClosurePeriod{}, fmt.Errorf("終了日: %w", err)
	}
	return domain.NewClosurePeriod(start, end, strings.TrimSpace(req.Reason))
}

// handleRemoveClosure は臨時休館期間を外す。{index} は GET /api/libraries の closures（開始日の順）の位置。
func (s *server) handleRemoveClosure(w http.ResponseWriter, r *http.Request) {
	id, ok := pathLibraryID(w, r)
	if !ok {
		return
	}
	index, ok := closureIndex(r.PathValue("index"))
	if !ok {
		writeClosureNotFound(w)
		return
	}
	l, err := s.updateLibrary(id, func(l *domain.RegisteredLibrary) error {
		closures := slices.Clone(l.Closures)
		domain.SortClosures(closures)
		if index >= len(closures) {
			return errClosureNotFound
		}
		l.Closures = slices.Delete(closures, index, index+1)
		return nil
	})
	s.writeLibraryUpdate(w, "臨時休館期間の削除", http.StatusOK, l, err)
}

// errClosureExists・errClosureNotFound は、同じ臨時休館期間がすでにあること・指定した位置に期間がないこと。
var (
	errClosureExists   = errors.New("同じ臨時休館期間があります")
	errClosureNotFound = errors.New("臨時休館期間がありません")
)

// closureIndex は {index}（0始まりの10進数）を読む。符号や数字以外を含めば false。
func closureIndex(s string) (int, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	i, err := strconv.Atoi(s)
	return i, err == nil
}

func writeClosureNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "notFound", "この臨時休館期間は登録されていません")
}
