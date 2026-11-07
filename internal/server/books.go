package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"libread/internal/domain"
	"libread/internal/ndl"
	"libread/internal/present"
	"libread/internal/store"
)

// 読みかけの本（contracts/http-api.md「読みかけの本」）。

type booksResponse struct {
	Books []bookJSON `json:"books"`
}

type bookJSON struct {
	ISBN      string `json:"isbn"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Publisher string `json:"publisher"`
	Note      string `json:"note"`
	AddedOn   string `json:"addedOn"`
}

func bookJSONOf(b domain.ReadingBook) bookJSON {
	return bookJSON{
		ISBN:      b.Book.ISBN.String(),
		Title:     b.Book.Title,
		Author:    b.Book.Author,
		Publisher: b.Book.Publisher,
		Note:      b.Note,
		AddedOn:   b.AddedOn.String(),
	}
}

// handleListBooks は読みかけの本を登録順に返す。
func (s *server) handleListBooks(w http.ResponseWriter, r *http.Request) {
	data, err := s.loadData()
	if err != nil {
		s.logger.Printf("利用者データを読めません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	out := booksResponse{Books: make([]bookJSON, 0, len(data.Books))}
	for _, b := range data.Books {
		out.Books = append(out.Books, bookJSONOf(b))
	}
	writeJSON(w, http.StatusOK, out)
}

// 書誌の検索（GET /api/book-search）

type bookSearchResponse struct {
	Books     []bookCandidateJSON `json:"books"`
	Total     int                 `json:"total"`
	Truncated bool                `json:"truncated"`
	Hint      string              `json:"hint,omitempty"`
	Credit    ndlCreditJSON       `json:"credit"`
}

type bookCandidateJSON struct {
	ISBN       string `json:"isbn"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Publisher  string `json:"publisher"`
	Registered bool   `json:"registered"`
}

type ndlCreditJSON struct {
	Label   string    `json:"label"`
	URL     string    `json:"url"`
	License labelLink `json:"license"`
}

type labelLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// ndlCredit は NDL サーチの利用条件で必須の出典表示（001 research R10）。
var ndlCredit = ndlCreditJSON{
	Label:   present.NDLCreditLabel,
	URL:     present.NDLURL,
	License: labelLink{Label: present.NDLLicenseLabel, URL: present.NDLLicenseURL},
}

// narrowingHint は、一致した本を表示しきれなかったときの案内。001 の book find の案内を画面の言葉にしたもの。
const narrowingHint = "著者や出版社で絞り込むか、ISBN で探してください"

// searchHint は 001 の book find と同じ条件で案内を作る。一致した本を表示しきれなかったときだけ付ける。
// 取得した中に ISBN のある本が1件もなければ、一致の件数とともにそのことを伝える。
func searchHint(res ndl.Result) string {
	switch {
	case !res.Truncated:
		return ""
	case len(res.Books) == 0:
		return fmt.Sprintf("%d件の一致がありますが、取得した中に ISBN のある本がありませんでした。%s", res.Total, narrowingHint)
	default:
		return "表示しきれていない本があります。見つからないときは" + narrowingHint
	}
}

// handleBookSearch は NDL サーチで書誌を探す（001 の book find と同じ。最大10件、並べ替えも同じ）。
func (s *server) handleBookSearch(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := ndl.Query{
		Text:      strings.TrimSpace(params.Get("q")),
		Author:    strings.TrimSpace(params.Get("author")),
		Publisher: strings.TrimSpace(params.Get("publisher")),
	}
	if q.Text == "" {
		writeError(w, http.StatusBadRequest, "invalid", "書名または ISBN を入力してください")
		return
	}
	var res ndl.Result
	err := s.withNDL(func(c *ndl.Client) error {
		var err error
		res, err = c.Search(r.Context(), q)
		return err
	})
	if err != nil {
		s.writeNDLError(w, "書誌の検索", err)
		return
	}
	// 登録済みかどうかは、問い合わせたあとのデータで決める。NDL サーチのロックを待つあいだに登録された本も含める。
	data, err := s.loadData()
	if err != nil {
		s.logger.Printf("利用者データを読めません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	out := bookSearchResponse{
		Books:     make([]bookCandidateJSON, 0, len(res.Books)),
		Total:     res.Total,
		Truncated: res.Truncated,
		Hint:      searchHint(res),
		Credit:    ndlCredit,
	}
	for _, b := range res.Books {
		_, registered := findBook(data.Books, b.ISBN)
		out.Books = append(out.Books, bookCandidateJSON{
			ISBN: b.ISBN.String(), Title: b.Title, Author: b.Author, Publisher: b.Publisher, Registered: registered,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// writeNDLError は NDL サーチに問い合わせられなかったことを 502 で返す。
// ログには要求の URL（検索語を含む）を書かない（research W11）。
func (s *server) writeNDLError(w http.ResponseWriter, what string, err error) {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	s.logger.Printf("%s: 国立国会図書館サーチ: %v", what, err)
	writeError(w, http.StatusBadGateway, "upstream", "国立国会図書館サーチに接続できませんでした。しばらくしてから再試行してください")
}

// 読みかけの本の登録・メモ・削除

// maxBookBodyBytes は本の登録・メモの要求の本文の上限。
const maxBookBodyBytes = 64 << 10

// errNotRegistered・errAlreadyRegistered は、本が読みかけの本にない・すでにあること。
var (
	errNotRegistered     = errors.New("登録されていません")
	errAlreadyRegistered = errors.New("登録済みです")
)

// decodeBody は要求の本文を JSON として v に読む。
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBookBodyBytes)).Decode(v)
}

type addBookRequest struct {
	ISBN string  `json:"isbn"`
	Note *string `json:"note"`
}

// handleAddBook は NDL サーチで ISBN の書誌を確かめてから、読みかけの本の最後に加える（001 の book add と同じ）。
func (s *server) handleAddBook(w http.ResponseWriter, r *http.Request) {
	var req addBookRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "要求の形式が正しくありません")
		return
	}
	isbn, err := domain.ParseISBN(strings.TrimSpace(req.ISBN))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	var note string
	if req.Note != nil {
		note = *req.Note
	}

	// 登録済みなら NDL サーチに問い合わせない。
	data, err := s.loadData()
	if err != nil {
		s.logger.Printf("利用者データを読めません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	if b, ok := findBook(data.Books, isbn); ok {
		writeAlreadyRegistered(w, b.Book)
		return
	}

	var found ndl.Result
	err = s.withNDL(func(c *ndl.Client) error {
		var err error
		found, err = c.Search(r.Context(), ndl.Query{Text: isbn.String()})
		return err
	})
	if err != nil {
		s.writeNDLError(w, "本の登録", err)
		return
	}
	i := slices.IndexFunc(found.Books, func(b domain.Book) bool { return b.ISBN == isbn })
	if i < 0 {
		writeError(w, http.StatusBadRequest, "invalid", fmt.Sprintf("ISBN %s の書誌が国立国会図書館サーチで見つかりませんでした", isbn))
		return
	}
	added := domain.ReadingBook{Book: found.Books[i], Note: note, AddedOn: domain.DateOf(s.cfg.Now())}

	var existing domain.Book
	_, err = s.updateData(func(d *store.Data) error {
		// NDL サーチに問い合わせているあいだに、同じ本が登録されたかもしれない。
		if b, ok := findBook(d.Books, isbn); ok {
			existing = b.Book
			return errAlreadyRegistered
		}
		d.Books = append(d.Books, added)
		return nil
	})
	switch {
	case errors.Is(err, errAlreadyRegistered):
		writeAlreadyRegistered(w, existing)
	case err != nil:
		s.writeSaveError(w, "本の登録", err)
	default:
		writeJSON(w, http.StatusCreated, bookJSONOf(added))
	}
}

func writeAlreadyRegistered(w http.ResponseWriter, b domain.Book) {
	writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("『%s』はすでに読みかけの本に登録されています", b.Title))
}

type updateNoteRequest struct {
	Note *string `json:"note"`
}

// handleUpdateNote はメモを書き換える。空文字ならメモを消す。
func (s *server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	isbn, ok := pathISBN(w, r)
	if !ok {
		return
	}
	var req updateNoteRequest
	if err := decodeBody(w, r, &req); err != nil || req.Note == nil {
		writeError(w, http.StatusBadRequest, "invalid", "要求の形式が正しくありません")
		return
	}
	var updated domain.ReadingBook
	_, err := s.updateData(func(d *store.Data) error {
		i := slices.IndexFunc(d.Books, func(b domain.ReadingBook) bool { return b.Book.ISBN == isbn })
		if i < 0 {
			return errNotRegistered
		}
		d.Books[i].Note = *req.Note
		updated = d.Books[i]
		return nil
	})
	switch {
	case errors.Is(err, errNotRegistered):
		writeBookNotFound(w)
	case err != nil:
		s.writeSaveError(w, "メモの保存", err)
	default:
		writeJSON(w, http.StatusOK, bookJSONOf(updated))
	}
}

// handleRemoveBook は読みかけの本から外す。
func (s *server) handleRemoveBook(w http.ResponseWriter, r *http.Request) {
	isbn, ok := pathISBN(w, r)
	if !ok {
		return
	}
	_, err := s.updateData(func(d *store.Data) error {
		i := slices.IndexFunc(d.Books, func(b domain.ReadingBook) bool { return b.Book.ISBN == isbn })
		if i < 0 {
			return errNotRegistered
		}
		d.Books = slices.Delete(d.Books, i, i+1)
		return nil
	})
	switch {
	case errors.Is(err, errNotRegistered):
		writeBookNotFound(w)
	case err != nil:
		s.writeSaveError(w, "本の削除", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// pathISBN は URL の {isbn} を読む。読めなければ 400 を返して false。
func pathISBN(w http.ResponseWriter, r *http.Request) (domain.ISBN, bool) {
	isbn, err := domain.ParseISBN(r.PathValue("isbn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return "", false
	}
	return isbn, true
}

func writeBookNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "notFound", "この本は読みかけの本に登録されていません")
}

// writeSaveError は利用者データを読み書きできなかったことを 500 で返す。
func (s *server) writeSaveError(w http.ResponseWriter, what string, err error) {
	s.logger.Printf("%s: %v", what, err)
	writeError(w, http.StatusInternalServerError, "internal", "保存できませんでした")
}
