package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"libread/internal/auth"
	"libread/internal/store"
)

// loginTTLSeconds は libread_login（ログインの途中）の有効期間（data-model.md LoginAttempt）。
const loginTTLSeconds = 10 * 60

// deniedLocation は本人と確認できなかったときの戻り先。本人以外か、それ以外の失敗かは区別しない。
const deniedLocation = "/#/login?error=denied"

func (s *server) authRoutes() {
	s.auth.HandleFunc("GET /auth/login", s.handleLogin)
	s.auth.HandleFunc("GET /auth/callback", s.handleCallback)
	s.api.HandleFunc("GET /api/session", s.handleGetSession)
	s.api.HandleFunc("POST /api/session/logout", s.handleLogout)
	s.api.HandleFunc("POST /api/session/revoke-all", s.handleRevokeAll)
}

// handleLogin は state を libread_login に入れ、Google の認可画面へリダイレクトする。
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ret := auth.ParseReturnPath(r.URL.Query().Get("return"))
	state, err := auth.NewState()
	if err != nil {
		s.logger.Printf("ログインを始められません: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     loginCookieName,
		Value:    state + "|" + ret.String(),
		Path:     "/auth/",
		MaxAge:   loginTTLSeconds,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.cfg.Auth.LoginURL(state), http.StatusFound)
}

// handleCallback は Google から戻った要求を確かめ、本人ならセッション Cookie を発行する。libread_login は必ず消す。
func (s *server) handleCallback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: loginCookieName, Path: "/auth/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})

	q := r.URL.Query()
	c, err := r.Cookie(loginCookieName)
	if err != nil {
		http.Redirect(w, r, deniedLocation, http.StatusFound)
		return
	}
	state, ret, ok := strings.Cut(c.Value, "|")
	got := q.Get("state")
	if !ok || state == "" || got == "" || subtle.ConstantTimeCompare([]byte(state), []byte(got)) != 1 ||
		q.Has("error") || q.Get("code") == "" {
		http.Redirect(w, r, deniedLocation, http.StatusFound)
		return
	}
	if err := s.cfg.Auth.Exchange(r.Context(), q.Get("code")); err != nil {
		s.logger.Printf("ログインを拒否しました: %v", err)
		http.Redirect(w, r, deniedLocation, http.StatusFound)
		return
	}
	gen, err := s.currentGeneration()
	if err != nil {
		s.logger.Printf("本人確認の状態を読めません: %v", err)
		http.Redirect(w, r, deniedLocation, http.StatusFound)
		return
	}
	value, sess, err := auth.Issue(s.cfg.SessionKey, gen, s.cfg.Now())
	if err != nil {
		s.logger.Printf("セッションを発行できません: %v", err)
		http.Redirect(w, r, deniedLocation, http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sess.ExpiresAt.Sub(sess.IssuedAt).Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/#"+auth.ParseReturnPath(ret).String(), http.StatusFound)
}

type sessionJSON struct {
	ExpiresAt string `json:"expiresAt"`
}

// handleGetSession は本人確認の状態を返す。メールアドレスは返さない。
func (s *server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sessionJSON{ExpiresAt: rfc3339(sessionOf(r).ExpiresAt)})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleRevokeAll はセッション世代番号を1つ進め、すべての端末（この端末を含む）の確認を取り消す（FR-111）。
func (s *server) handleRevokeAll(w http.ResponseWriter, r *http.Request) {
	if err := s.advanceGeneration(); err != nil {
		s.logger.Printf("確認を取り消せません: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "確認を取り消せませんでした")
		return
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// advanceGeneration は、利用者データの書き込みと同じロックの中で auth.json の世代番号を進める。
func (s *server) advanceGeneration() error {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	st, err := store.LoadAuthState(s.cfg.DataDir)
	if err != nil {
		return err
	}
	gen, err := auth.NewSessionGeneration(st.Generation)
	if err != nil {
		return err
	}
	return store.SaveAuthState(s.cfg.DataDir, store.AuthState{Generation: gen.Next().Int()})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}
