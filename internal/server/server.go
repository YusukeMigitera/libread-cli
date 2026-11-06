// Package server は libread の HTTP サーバー。API（contracts/http-api.md）と、ビルド済みの画面の配信を行う。
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"libread/internal/auth"
	"libread/internal/calil"
	"libread/internal/domain"
	"libread/internal/holiday"
	"libread/internal/ndl"
	"libread/internal/store"
)

// userAgent は外部サービスへの要求に付ける（001 research R9）。
const userAgent = "libread/0.2.0"

const (
	sessionCookieName = "libread_session"
	loginCookieName   = "libread_login"
)

// Config はサーバーの設定。cmd/libread-server が環境変数から組み立てる。
type Config struct {
	DataDir     string
	CacheDir    string
	Static      fs.FS // ビルド済みの画面（web/dist）。nil なら画面は配信しない
	Auth        auth.Config
	SessionKey  []byte
	CalilAppKey string // 空なら、カーリルを使う API は 503 notConfigured
	Now         func() time.Time
	// Sleep はカーリルのポーリングの間隔を待つ。nil なら実際に待つ。テストで時刻を進めるのに使う。
	Sleep func(ctx context.Context, d time.Duration) error
	HTTP  *http.Client // 外部サービス（カーリル・NDL サーチ・holidays-jp）への要求に使う
	Log   io.Writer    // 1要求1行のログと、外部サービスのエラーの書き先
}

type server struct {
	cfg    Config
	api    *http.ServeMux
	auth   *http.ServeMux
	logger *log.Logger

	// dataMu は data.json と auth.json の「読み込み → 変更 → 書き込み」を直列にする（research W3）。
	dataMu sync.Mutex
	// 外部サービスへの要求は、サービスごとに同時に1つまでにする（research W9）。
	// ロックは、そのサービスに問い合わせているあいだだけ持つ。
	calilMu    sync.Mutex // 所蔵チェック・図書館検索・図書館の登録で共有する
	ndlMu      sync.Mutex // 書誌検索・本の登録で共有する
	holidaysMu sync.Mutex
}

// New は設定に従って HTTP ハンドラーを作る。
func New(cfg Config) http.Handler {
	s := newServer(cfg)
	return s.withSecurityHeaders(s.withRequestLog(http.HandlerFunc(s.dispatch)))
}

// newServer は設定の既定値を補い、URL を登録したサーバーを作る。
func newServer(cfg Config) *server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.Log == nil {
		cfg.Log = io.Discard
	}
	s := &server{
		cfg:    cfg,
		api:    http.NewServeMux(),
		auth:   http.NewServeMux(),
		logger: log.New(cfg.Log, "", 0),
	}
	s.routes()
	return s
}

// routes は API と本人確認の URL を登録する。
func (s *server) routes() {
	s.authRoutes()
	s.api.HandleFunc("GET /api/check", s.handleCheck)
	s.api.HandleFunc("POST /api/import", s.handleImport)
}

// dispatch は URL の先頭で、API・本人確認・画面に振り分ける。
func (s *server) dispatch(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/"):
		w.Header().Set("Cache-Control", "no-store")
		s.serveAPI(w, r)
	case strings.HasPrefix(r.URL.Path, "/auth/"):
		w.Header().Set("Cache-Control", "no-store")
		s.auth.ServeHTTP(w, r)
	default:
		s.serveStatic(w, r)
	}
}

// serveAPI は、URL の存在を確かめる前に本人確認を行う。確認できなければ、どの URL・メソッドでも同じ 401 を返す（FR-110）。
// 状態を変える要求は、Origin が自分のオリジンと一致するときだけ受け付ける（research W2）。
func (s *server) serveAPI(w http.ResponseWriter, r *http.Request) {
	sess, err := s.verifySession(r)
	if errors.Is(err, errStorage) {
		// 保存先の失敗は本人確認の失敗とは扱わない。Cookie のない相手はここに来ない（一律の 401 のまま）。
		writeError(w, http.StatusInternalServerError, "internal", "保存先を読めませんでした")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "ログインしてください")
		return
	}
	if isWrite(r.Method) && r.Header.Get("Origin") != s.cfg.Auth.PublicOrigin {
		writeError(w, http.StatusForbidden, "forbidden", "この操作は受け付けられません")
		return
	}
	h, pattern := s.api.Handler(r)
	if pattern == "" {
		writeError(w, http.StatusNotFound, "notFound", "見つかりません")
		return
	}
	h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)))
}

type sessionKey struct{}

// sessionOf は本人確認を通った要求のセッション。
func sessionOf(r *http.Request) auth.Session {
	s, _ := r.Context().Value(sessionKey{}).(auth.Session)
	return s
}

// errStorage は auth.json を読めないこと（保存先の失敗）。
var errStorage = errors.New("本人確認の状態を読めません")

// errNoSession は有効なセッションがないこと（Cookie なし・無効・期限切れ・世代が古い）。理由は区別しない。
var errNoSession = errors.New("セッションがありません")

// verifySession は要求のセッション Cookie を確かめる。Cookie がなければ、保存先を読まずに errNoSession を返す。
// Cookie があり auth.json を読めなければ errStorage を返す。
func (s *server) verifySession(r *http.Request) (auth.Session, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return auth.Session{}, errNoSession
	}
	gen, err := s.currentGeneration()
	if err != nil {
		return auth.Session{}, err
	}
	sess, err := auth.Verify(s.cfg.SessionKey, c.Value, gen, s.cfg.Now())
	if err != nil {
		return auth.Session{}, errNoSession
	}
	return sess, nil
}

// currentGeneration は auth.json の世代番号。読めなければ errStorage を返し、理由をログに書く。
func (s *server) currentGeneration() (auth.SessionGeneration, error) {
	st, err := store.LoadAuthState(s.cfg.DataDir)
	if err == nil {
		var gen auth.SessionGeneration
		if gen, err = auth.NewSessionGeneration(st.Generation); err == nil {
			return gen, nil
		}
	}
	s.logger.Printf("%v: %v", errStorage, err)
	return 0, errStorage
}

func isWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// 利用者データ（research W3）

// loadData は最新の利用者データを読む。
func (s *server) loadData() (store.Data, error) {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	return store.Load(s.cfg.DataDir)
}

// updateData は、最新の利用者データを読み、change を1つ当てて保存する。すべてロックの中で行う。
// change がエラーを返したら保存しない。
func (s *server) updateData(change func(*store.Data) error) (store.Data, error) {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	d, err := store.Load(s.cfg.DataDir)
	if err != nil {
		return store.Data{}, err
	}
	if err := change(&d); err != nil {
		return store.Data{}, err
	}
	if err := store.Save(s.cfg.DataDir, d); err != nil {
		return store.Data{}, err
	}
	return d, nil
}

// 外部サービス（research W9）

// errNotConfigured はカーリルのアプリケーションキーが設定されていないこと。API は 503 notConfigured を返す。
var errNotConfigured = errors.New("カーリルのアプリケーションキーが設定されていません")

// withCalil はカーリルへの問い合わせを、ほかのカーリルへの問い合わせと重ならないように行う。
func (s *server) withCalil(fn func(*calil.Client) error) error {
	if s.cfg.CalilAppKey == "" {
		return errNotConfigured
	}
	s.calilMu.Lock()
	defer s.calilMu.Unlock()
	return fn(&calil.Client{AppKey: s.cfg.CalilAppKey, HTTP: s.cfg.HTTP, Now: s.cfg.Now, Sleep: s.cfg.Sleep, UserAgent: userAgent})
}

// withNDL は NDL サーチへの問い合わせを、ほかの NDL サーチへの問い合わせと重ならないように行う。
func (s *server) withNDL(fn func(*ndl.Client) error) error {
	s.ndlMu.Lock()
	defer s.ndlMu.Unlock()
	return fn(&ndl.Client{HTTP: s.cfg.HTTP, UserAgent: userAgent})
}

// withHolidays は holidays-jp への問い合わせを、ほかの holidays-jp への問い合わせと重ならないように行う。
func (s *server) withHolidays(fn func(*holiday.Client) error) error {
	s.holidaysMu.Lock()
	defer s.holidaysMu.Unlock()
	return fn(&holiday.Client{HTTP: s.cfg.HTTP, Cache: store.NewCache(s.cfg.CacheDir), Now: s.cfg.Now, UserAgent: userAgent})
}

// JSON（contracts/http-api.md「共通」）

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "応答を作れませんでした")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError はエラーの共通の形で応答する。message は画面にそのまま表示されるので、アプリケーションキーなどを含めない。
func writeError(w http.ResponseWriter, status int, code, message string) {
	raw, _ := json.Marshal(errorBody{Error: errorDetail{Code: code, Message: message}})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(append(raw, '\n'))
}

// rfc3339 は日時を日本時間の RFC 3339 で表す（contracts/http-api.md「共通」）。
func rfc3339(t time.Time) string { return t.In(domain.Tokyo()).Format(time.RFC3339) }

// 共通のヘッダーとログ（research W10・W11）

func (s *server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		next.ServeHTTP(w, r)
	})
}

// withRequestLog は1要求ごとに「時刻 メソッド パス 状態コード 処理時間」を1行で書く。
// クエリー文字列・Cookie・本文は書かない（検索語・メモ・トークンを含むため）。
func (s *server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Printf("%s %s %s %d %s", start.In(domain.Tokyo()).Format(time.RFC3339), r.Method, logPath(r), rec.status, time.Since(start))
	})
}

// logPath はログに書くパス。空白などでログの1行が崩れないよう、エスケープした形を使う。
func logPath(r *http.Request) string {
	p := r.URL.EscapedPath()
	if p == "" {
		return "/"
	}
	return p
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// 静的ファイル（contracts/http-api.md「静的ファイル」）

// serveStatic はビルド済みの画面を返す。本人確認は要らない（利用者データを含まないため）。
// index.html は毎回確かめさせ（no-cache）、ファイル名にハッシュの付いた /assets/ は1年キャッシュさせる。
func (s *server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if s.cfg.Static == nil || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(s.cfg.Static, name)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	raw, err := fs.ReadFile(s.cfg.Static, name)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	switch {
	case name == "index.html":
		w.Header().Set("Cache-Control", "no-cache")
	case strings.HasPrefix(name, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(raw))
}
