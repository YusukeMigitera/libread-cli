package server

import (
	"errors"
	"io"
	"net/http"

	"libread/internal/store"
)

// maxImportBytes は取り込む data.json の大きさの上限（contracts/http-api.md「取り込み」）。
const maxImportBytes = 1 << 20

type importResponse struct {
	Books     int `json:"books"`
	Libraries int `json:"libraries"`
	Closures  int `json:"closures"`
}

// errAlreadyHasData は、すでに読みかけの本か利用図書館があること。取り込みは 409 にする。
var errAlreadyHasData = errors.New("すでにデータがあります")

// handleImport は 001 の data.json の中身をそのまま受け取り、クラウド上のデータが空のときだけ保存する（FR-118）。
// 形式が正しくなければ何も保存しない。
func (s *server) handleImport(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "data.json は 1 MiB 以下にしてください")
		return
	}
	in, err := store.ParseData(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "data.json の形式が正しくありません: "+err.Error())
		return
	}
	_, err = s.updateData(func(d *store.Data) error {
		if len(d.Books) > 0 || len(d.Libraries) > 0 {
			return errAlreadyHasData
		}
		*d = in
		return nil
	})
	switch {
	case errors.Is(err, errAlreadyHasData):
		writeError(w, http.StatusConflict, "conflict", "すでに読みかけの本か利用図書館が登録されているため、取り込めません")
		return
	case err != nil:
		s.logger.Printf("取り込み: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存できませんでした")
		return
	}
	res := importResponse{Books: len(in.Books), Libraries: len(in.Libraries)}
	for _, l := range in.Libraries {
		res.Closures += len(l.Closures)
	}
	writeJSON(w, http.StatusOK, res)
}
