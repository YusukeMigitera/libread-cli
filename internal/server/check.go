package server

import (
	"net/http"

	"libread/internal/checkrun"
	"libread/internal/domain"
	"libread/internal/present"
	"libread/internal/store"
)

// GET /api/check（contracts/http-api.md「検索」）の応答。

type setupRequiredResponse struct {
	SetupRequired string `json:"setupRequired"` // "libraries" または "books"
}

type checkResponse struct {
	CheckedAt   string             `json:"checkedAt"`
	Readable    []checkLibraryJSON `json:"readable"`
	NotReadable []checkLibraryJSON `json:"notReadable"`
	Sources     checkSourcesJSON   `json:"sources"`
}

type checkLibraryJSON struct {
	Library libraryRefJSON  `json:"library"`
	Opening openingJSON     `json:"opening"`
	Books   []checkBookJSON `json:"books"`
}

type libraryRefJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// openingJSON の open・close は開館中・開館前・本日終了のときだけ、closingSoon は開館中のときだけ付ける。
type openingJSON struct {
	Status      string `json:"status"`
	Label       string `json:"label"`
	Open        string `json:"open,omitempty"`
	Close       string `json:"close,omitempty"`
	ClosingSoon *bool  `json:"closingSoon,omitempty"`
}

// checkBookJSON の fetchedAt は、所蔵状況を取得できなかった本では付けない。
type checkBookJSON struct {
	ISBN         string `json:"isbn"`
	Title        string `json:"title"`
	Availability string `json:"availability"`
	Label        string `json:"label"`
	Readable     bool   `json:"readable"`
	Stale        bool   `json:"stale"`
	FetchedAt    string `json:"fetchedAt,omitempty"`
}

type checkSourcesJSON struct {
	Holdings    holdingsSourceJSON    `json:"holdings"`
	OpeningInfo openingInfoSourceJSON `json:"openingInfo"`
	Holidays    labelJSON             `json:"holidays"`
}

type holdingsSourceJSON struct {
	Label string     `json:"label"`
	Links []linkJSON `json:"links"`
}

type linkJSON struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type openingInfoSourceJSON struct {
	Label string `json:"label"`
	Note  string `json:"note"`
}

type labelJSON struct {
	Label string `json:"label"`
}

// handleCheck は読みかけの本すべて（?isbn= なら1冊）について、今日読める図書館を返す（FR-101〜FR-105）。
// 呼ぶたびに今の時刻で判定し直す。所蔵状況は30分以内のキャッシュを使い、なければカーリルに1セッションで問い合わせる。
func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	data, err := s.loadData()
	if err != nil {
		s.logger.Printf("利用者データを読めません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	if len(data.Libraries) == 0 {
		writeJSON(w, http.StatusOK, setupRequiredResponse{SetupRequired: "libraries"})
		return
	}
	if len(data.Books) == 0 {
		writeJSON(w, http.StatusOK, setupRequiredResponse{SetupRequired: "books"})
		return
	}
	books := data.Books
	if q := r.URL.Query(); q.Has("isbn") {
		isbn, err := domain.ParseISBN(q.Get("isbn"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid", "ISBN が正しくありません")
			return
		}
		b, ok := findBook(data.Books, isbn)
		if !ok {
			writeError(w, http.StatusNotFound, "notFound", "この本は読みかけの本に登録されていません")
			return
		}
		books = []domain.ReadingBook{b}
	}

	c, err := s.calilClient()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "notConfigured", "カーリルのアプリケーションキーが設定されていません")
		return
	}
	now := s.cfg.Now()
	// カーリルのロックは、所蔵キャッシュを読むところから保存するところまでだけ持つ（checkrun.Deps.CalilLock）。
	// 同時に来た2つ目の検索は1つ目を待ち、1つ目が保存した30分以内のキャッシュを使う。
	// 祝日のロックは開館状態の判定のあいだだけ持つ。2つのロックを同時には持たない（research W9）。
	deps := checkrun.Deps{
		Calil:        c,
		Holidays:     s.holidaysClient(),
		Cache:        store.NewCache(s.cfg.CacheDir),
		CalilLock:    &s.calilMu,
		HolidaysLock: &s.holidaysMu,
		Warn:         func(msg string) { s.logger.Printf("検索: %s", msg) },
	}
	result, err := checkrun.Run(r.Context(), deps, data, books, now)
	if err != nil {
		s.logger.Printf("検索: %v", err)
		writeError(w, http.StatusBadGateway, "upstream", "カーリルに接続できませんでした。しばらくしてから再試行してください")
		return
	}
	writeJSON(w, http.StatusOK, checkResponseOf(result, books, data.Libraries))
}

func findBook(books []domain.ReadingBook, isbn domain.ISBN) (domain.ReadingBook, bool) {
	for _, b := range books {
		if b.Book.ISBN == isbn {
			return b, true
		}
	}
	return domain.ReadingBook{}, false
}

// checkResponseOf は検索結果を契約の形にする。文言は 001 の CLI と同じ（internal/present、SC-103）。
// books は調べた本、libs は利用図書館（登録順）。
func checkResponseOf(r domain.Result, books []domain.ReadingBook, libs []domain.RegisteredLibrary) checkResponse {
	perBook := present.PerBookTimes(r)
	links := make([]linkJSON, 0, len(books))
	for _, b := range books {
		links = append(links, linkJSON{Title: b.Book.Title, URL: present.CalilBookURL(b.Book.ISBN)})
	}
	return checkResponse{
		CheckedAt:   rfc3339(r.CheckedAt),
		Readable:    librariesJSON(r.Readable, perBook, false),
		NotReadable: librariesJSON(r.NotReadable, perBook, true),
		Sources: checkSourcesJSON{
			Holdings:    holdingsSourceJSON{Label: present.HoldingsSourceLabel(r), Links: links},
			OpeningInfo: openingInfoSourceJSON{Label: present.OpeningInfoSourceLabel(libs), Note: present.ClosureNote},
			Holidays:    labelJSON{Label: present.HolidaysLabel},
		},
	}
}

// librariesJSON は館の一覧を契約の形にする。読めない館（notReadable）のうち今日行けない館は、
// 001 の CLI と同じく開館状態だけを理由に示し、本ごとの所蔵状況は省く。
func librariesJSON(in []domain.LibraryResult, perBook, notReadable bool) []checkLibraryJSON {
	out := make([]checkLibraryJSON, 0, len(in))
	for _, l := range in {
		lib := checkLibraryJSON{
			Library: libraryRefJSON{ID: l.Library.ID.String(), Name: l.Library.Name},
			Opening: openingOf(l.Opening),
			Books:   []checkBookJSON{},
		}
		if st := l.Opening.Status; !notReadable || st == domain.Open || st == domain.NotYetOpen {
			for _, h := range l.Books {
				lib.Books = append(lib.Books, bookOf(h, l.Opening.Status, perBook))
			}
		}
		out = append(out, lib)
	}
	return out
}

func openingOf(o domain.Opening) openingJSON {
	j := openingJSON{Status: openStatusNames[o.Status], Label: present.OpeningLabel(o)}
	switch o.Status {
	case domain.Open, domain.NotYetOpen, domain.FinishedToday:
		j.Open = o.Hours.Open().String()
		j.Close = o.Hours.Close().String()
	}
	if o.Status == domain.Open {
		soon := o.ClosingSoon
		j.ClosingSoon = &soon
	}
	return j
}

func bookOf(h domain.BookHolding, open domain.OpenStatus, perBook bool) checkBookJSON {
	j := checkBookJSON{
		ISBN:         h.Book.ISBN.String(),
		Title:        h.Book.Title,
		Availability: availabilityNames[h.Availability],
		Label:        present.HoldingLabel(h, perBook),
		Readable:     domain.Readable(open, h.Availability, h.Stale),
		Stale:        h.Stale,
	}
	if (h.SystemStatus == domain.SystemOK || h.SystemStatus == domain.SystemCache) && !h.FetchedAt.IsZero() {
		j.FetchedAt = rfc3339(h.FetchedAt)
	}
	return j
}

// openStatusNames・availabilityNames は契約での値（001 data-model の OpenStatus・Availability）。
var openStatusNames = map[domain.OpenStatus]string{
	domain.NoSchedule:     "noSchedule",
	domain.Open:           "open",
	domain.NotYetOpen:     "notYetOpen",
	domain.FinishedToday:  "finishedToday",
	domain.ClosedRegular:  "closedRegular",
	domain.ClosedPeriod:   "closedPeriod",
	domain.HolidayUnknown: "holidayUnknown",
}

var availabilityNames = map[domain.Availability]string{
	domain.Unknown:         "unknown",
	domain.AvailableToLend: "availableToLend",
	domain.InLibraryOnly:   "inLibraryOnly",
	domain.OnLoan:          "onLoan",
	domain.Reserved:        "reserved",
	domain.NotHeld:         "notHeld",
}
