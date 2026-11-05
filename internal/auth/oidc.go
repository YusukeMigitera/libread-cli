package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Google の OpenID Connect のエンドポイント。
const (
	GoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
)

// CallbackPath は Google から戻る先。リダイレクト URI は PublicOrigin + CallbackPath。
const CallbackPath = "/auth/callback"

// Config は Google でのログインの設定（research W2）。
type Config struct {
	ClientID     string
	ClientSecret string
	PublicOrigin string // 例: https://libread-123.asia-northeast1.run.app（末尾の / なし）
	OwnerEmail   string // 本人の Google アカウント（LIBREAD_OWNER_EMAIL）
	AuthURL      string // 空なら GoogleAuthURL
	TokenURL     string // 空なら GoogleTokenURL
	HTTP         *http.Client
	Now          func() time.Time // nil なら time.Now
}

// ErrDenied は本人と確認できなかったこと（本人以外、トークンの不備、Google のエラー）を表す。理由は利用者に区別して見せない。
var ErrDenied = errors.New("本人と確認できませんでした")

func (c Config) redirectURI() string { return c.PublicOrigin + CallbackPath }

// LoginURL は Google の認可画面の URL。scope は openid と email だけ。
func (c Config) LoginURL(state string) string {
	base := c.AuthURL
	if base == "" {
		base = GoogleAuthURL
	}
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {c.ClientID},
		"redirect_uri":  {c.redirectURI()},
		"scope":         {"openid email"},
		"state":         {state},
		"prompt":        {"select_account"},
	}
	return base + "?" + q.Encode()
}

// Exchange は認可コードをトークンエンドポイントで ID トークンに替え、本人のものか確かめる。本人なら nil。
//
// ID トークンはトークンエンドポイントから TLS で直接受け取るので、署名は検証しない
// （OpenID Connect Core 1.0 §3.1.3.7、research W2）。
func (c Config) Exchange(ctx context.Context, code string) error {
	tokenURL := c.TokenURL
	if tokenURL == "" {
		tokenURL = GoogleTokenURL
	}
	form := url.Values{
		"code":          {code},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"redirect_uri":  {c.redirectURI()},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("トークンの要求を作れません: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%w: トークンエンドポイントに接続できません", ErrDenied)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: トークンエンドポイントが %s を返しました", ErrDenied, resp.Status)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil {
		return fmt.Errorf("%w: トークンの形式が正しくありません", ErrDenied)
	}
	claims, err := decodeIDToken(tok.IDToken)
	if err != nil {
		return err
	}
	return c.checkOwner(claims)
}

type idClaims struct {
	Iss           string          `json:"iss"`
	Aud           json.RawMessage `json:"aud"` // 文字列か文字列の配列
	Exp           *int64          `json:"exp"`
	Email         string          `json:"email"`
	EmailVerified *bool           `json:"email_verified"`
}

func decodeIDToken(tok string) (idClaims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return idClaims{}, fmt.Errorf("%w: ID トークンの形式が正しくありません", ErrDenied)
	}
	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return idClaims{}, fmt.Errorf("%w: ID トークンの形式が正しくありません", ErrDenied)
	}
	var c idClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return idClaims{}, fmt.Errorf("%w: ID トークンの形式が正しくありません", ErrDenied)
	}
	return c, nil
}

func (c Config) checkOwner(claims idClaims) error {
	if claims.Iss != "https://accounts.google.com" && claims.Iss != "accounts.google.com" {
		return fmt.Errorf("%w: iss が Google ではありません", ErrDenied)
	}
	if !audienceContains(claims.Aud, c.ClientID) {
		return fmt.Errorf("%w: aud がこのクライアントではありません", ErrDenied)
	}
	if claims.Exp == nil || !c.now().Before(time.Unix(*claims.Exp, 0)) {
		return fmt.Errorf("%w: ID トークンの期限が切れています", ErrDenied)
	}
	if claims.EmailVerified == nil || !*claims.EmailVerified {
		return fmt.Errorf("%w: メールアドレスが確認されていません", ErrDenied)
	}
	if claims.Email == "" || c.OwnerEmail == "" || !strings.EqualFold(claims.Email, c.OwnerEmail) {
		return fmt.Errorf("%w: 本人のアカウントではありません", ErrDenied)
	}
	return nil
}

func audienceContains(raw json.RawMessage, clientID string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == clientID
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == clientID {
				return true
			}
		}
	}
	return false
}

func (c Config) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// NewState はログインの照合に使うランダム値（crypto/rand の32バイトを base64url にしたもの）。
func NewState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("ランダム値を作れません: %w", err)
	}
	return b64.EncodeToString(buf), nil
}

// ReturnPath はログイン後に戻る画面（data-model.md LoginAttempt）。決まった値の中からしか選べない。
type ReturnPath string

var returnPaths = []ReturnPath{"/", "/books", "/libraries"}

// ParseReturnPath は s が戻り先として許した値ならそれを、そうでなければ "/" を返す。
func ParseReturnPath(s string) ReturnPath {
	for _, p := range returnPaths {
		if string(p) == s {
			return p
		}
	}
	return "/"
}

func (p ReturnPath) String() string { return string(p) }
