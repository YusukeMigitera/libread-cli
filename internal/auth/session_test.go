package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var (
	testKey  = []byte("0123456789abcdef0123456789abcdef")
	issuedAt = time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
)

func mustGen(t *testing.T, n int) SessionGeneration {
	t.Helper()
	g, err := NewSessionGeneration(n)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func sign(key []byte, payload []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac.Sum(nil))
}

func TestNewSessionGeneration(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := NewSessionGeneration(n); err == nil {
			t.Errorf("NewSessionGeneration(%d): want error", n)
		}
	}
	if g, err := NewSessionGeneration(3); err != nil || g.Int() != 3 {
		t.Errorf("NewSessionGeneration(3) = %v, %v", g, err)
	}
}

func TestIssueFormatAndExpiry(t *testing.T) {
	value, s, err := Issue(testKey, mustGen(t, 2), issuedAt)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !s.IssuedAt.Equal(issuedAt) || !s.ExpiresAt.Equal(issuedAt.Add(30*24*time.Hour)) || s.Generation != mustGen(t, 2) {
		t.Errorf("session = %+v", s)
	}
	payloadB64, sigB64, ok := strings.Cut(value, ".")
	if !ok {
		t.Fatalf("value %q に . がない", value)
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("payload が base64url でない: %v", err)
	}
	if !json.Valid(payload) {
		t.Errorf("payload が JSON でない: %s", payload)
	}
	if want := sign(testKey, payload); value != want {
		t.Errorf("署名が HMAC-SHA256(key, JSON) でない\n got %s\nwant %s", value, want)
	}
	_ = sigB64
}

func TestIssueRejectsInvalidGeneration(t *testing.T) {
	if _, _, err := Issue(testKey, SessionGeneration(0), issuedAt); err == nil {
		t.Error("want error")
	}
}

func TestVerifyAcceptsFreshCookie(t *testing.T) {
	value, want, err := Issue(testKey, mustGen(t, 1), issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(testKey, value, mustGen(t, 1), issuedAt.Add(29*24*time.Hour))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.IssuedAt.Equal(want.IssuedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) || got.Generation != want.Generation {
		t.Errorf("Verify = %+v, want %+v", got, want)
	}
}

func TestVerifyRejects(t *testing.T) {
	value, s, err := Issue(testKey, mustGen(t, 2), issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	payloadB64, sigB64, _ := strings.Cut(value, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(payloadB64)
	enc := base64.RawURLEncoding

	tampered := strings.Replace(string(payload), `"generation":2`, `"generation":3`, 1)
	if tampered == string(payload) {
		t.Fatalf("payload %s に generation がない", payload)
	}
	otherSig := enc.EncodeToString([]byte("not-the-signature-not-the-signat"))

	tests := []struct {
		name    string
		key     []byte
		value   string
		current int
		now     time.Time
	}{
		{"中身の書き換え", testKey, enc.EncodeToString([]byte(tampered)) + "." + sigB64, 3, issuedAt},
		{"署名の書き換え", testKey, payloadB64 + "." + otherSig, 2, issuedAt},
		{"別の鍵", []byte("another-key-another-key-another-k"), value, 2, issuedAt},
		{"期限ちょうど", testKey, value, 2, s.ExpiresAt},
		{"期限後", testKey, value, 2, s.ExpiresAt.Add(time.Second)},
		{"世代が古い", testKey, value, 3, issuedAt},
		{"世代が新しい", testKey, value, 1, issuedAt},
		{". がない", testKey, payloadB64, 2, issuedAt},
		{"空", testKey, "", 2, issuedAt},
		{"base64 でない", testKey, "!!!." + sigB64, 2, issuedAt},
		{"JSON でない", testKey, sign(testKey, []byte("not json")), 2, issuedAt},
		{"世代が0", testKey, sign(testKey, []byte(strings.Replace(string(payload), `"generation":2`, `"generation":0`, 1))), 2, issuedAt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Verify(tt.key, tt.value, mustGen(t, tt.current), tt.now); err == nil {
				t.Error("want error")
			}
		})
	}
}
