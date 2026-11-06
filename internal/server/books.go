package server

import (
	"net/http"

	"libread/internal/domain"
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
