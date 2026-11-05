// Package auth は本人確認を行う。Google の OpenID Connect でのログインと、確認済み端末のセッション Cookie（research W2）。
// 標準ライブラリだけで作る（Constitution 原則 I）。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionTTL は確認済み端末の有効期間（FR-111）。
const SessionTTL = 30 * 24 * time.Hour

// SessionGeneration はセッション世代番号（1以上）。全端末の確認を取り消すと1つ進む（data-model.md AuthState）。
type SessionGeneration int

// NewSessionGeneration は n を世代番号にする。1未満はエラー。
func NewSessionGeneration(n int) (SessionGeneration, error) {
	if n < 1 {
		return 0, fmt.Errorf("セッション世代番号 %d は1以上にしてください", n)
	}
	return SessionGeneration(n), nil
}

// Int は世代番号を int で返す（auth.json への保存用）。
func (g SessionGeneration) Int() int { return int(g) }

// Next は次の世代番号。
func (g SessionGeneration) Next() SessionGeneration { return g + 1 }

// Session は確認済み端末（data-model.md Session）。サーバーには保存せず、署名付き Cookie に入れる。
type Session struct {
	IssuedAt   time.Time
	ExpiresAt  time.Time
	Generation SessionGeneration
}

// ErrInvalidSession は Cookie が無効（形式・署名・期限・世代のどれか）であることを表す。理由は区別しない。
var ErrInvalidSession = errors.New("セッションが無効です")

type sessionPayload struct {
	IssuedAt   int64 `json:"issuedAt"`
	ExpiresAt  int64 `json:"expiresAt"`
	Generation int   `json:"generation"`
}

var b64 = base64.RawURLEncoding

// Issue は now に本人を確認した端末のセッションを発行し、Cookie の値
// base64url(JSON) + "." + base64url(HMAC-SHA256(key, JSON)) を返す。
func Issue(key []byte, gen SessionGeneration, now time.Time) (string, Session, error) {
	if gen < 1 {
		return "", Session{}, fmt.Errorf("セッション世代番号 %d は1以上にしてください", gen)
	}
	issued := now.Truncate(time.Second)
	s := Session{IssuedAt: issued, ExpiresAt: issued.Add(SessionTTL), Generation: gen}
	payload, err := json.Marshal(sessionPayload{
		IssuedAt:   s.IssuedAt.Unix(),
		ExpiresAt:  s.ExpiresAt.Unix(),
		Generation: int(gen),
	})
	if err != nil {
		return "", Session{}, fmt.Errorf("セッションを作れません: %w", err)
	}
	return b64.EncodeToString(payload) + "." + b64.EncodeToString(mac(key, payload)), s, nil
}

// Verify は Cookie の値を検証する。署名が正しく、now < ExpiresAt で、世代が current と同じときだけ有効。
func Verify(key []byte, value string, current SessionGeneration, now time.Time) (Session, error) {
	payloadB64, sigB64, ok := strings.Cut(value, ".")
	if !ok {
		return Session{}, ErrInvalidSession
	}
	payload, err := b64.DecodeString(payloadB64)
	if err != nil {
		return Session{}, ErrInvalidSession
	}
	sig, err := b64.DecodeString(sigB64)
	if err != nil || !hmac.Equal(sig, mac(key, payload)) {
		return Session{}, ErrInvalidSession
	}
	var p sessionPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Session{}, ErrInvalidSession
	}
	gen, err := NewSessionGeneration(p.Generation)
	if err != nil || gen != current {
		return Session{}, ErrInvalidSession
	}
	s := Session{IssuedAt: time.Unix(p.IssuedAt, 0), ExpiresAt: time.Unix(p.ExpiresAt, 0), Generation: gen}
	if !now.Before(s.ExpiresAt) {
		return Session{}, ErrInvalidSession
	}
	return s, nil
}

func mac(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	return m.Sum(nil)
}
