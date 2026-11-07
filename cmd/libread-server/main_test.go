package main

import (
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{
		"LIBREAD_DATA_DIR":             "/data/data",
		"LIBREAD_CACHE_DIR":            "/data/cache",
		"LIBREAD_STATIC_DIR":           "/app/static",
		"LIBREAD_PUBLIC_ORIGIN":        "https://libread.example",
		"LIBREAD_OWNER_EMAIL":          "owner@example.com",
		"LIBREAD_GOOGLE_CLIENT_ID":     "client-123",
		"LIBREAD_GOOGLE_CLIENT_SECRET": "secret-456",
		"LIBREAD_SESSION_KEY":          "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", // 32バイト
	}
}

func TestLoadConfig(t *testing.T) {
	env := validEnv()
	env["LIBREAD_CALIL_APPKEY"] = "appkey"
	addr, cfg, err := loadConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if addr != ":8080" {
		t.Errorf("addr = %q, want :8080", addr)
	}
	if len(cfg.SessionKey) != 32 || cfg.Auth.PublicOrigin != "https://libread.example" || cfg.Auth.OwnerEmail != "owner@example.com" ||
		cfg.Auth.ClientID != "client-123" || cfg.Auth.ClientSecret != "secret-456" || cfg.CalilAppKey != "appkey" ||
		cfg.DataDir != "/data/data" || cfg.CacheDir != "/data/cache" || cfg.Static == nil {
		t.Errorf("cfg = %+v", cfg)
	}

	env["PORT"] = "9000"
	if addr, _, _ := loadConfig(func(k string) string { return env[k] }); addr != ":9000" {
		t.Errorf("addr = %q, want :9000", addr)
	}
}

func TestLoadConfigWithoutCalilKey(t *testing.T) {
	env := validEnv()
	if _, cfg, err := loadConfig(func(k string) string { return env[k] }); err != nil || cfg.CalilAppKey != "" {
		t.Errorf("カーリルのキーなしで起動できない: %v", err)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	for name := range validEnv() {
		t.Run(name+"なし", func(t *testing.T) {
			env := validEnv()
			delete(env, name)
			_, _, err := loadConfig(func(k string) string { return env[k] })
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("err = %v, want %s を含むエラー", err, name)
			}
		})
	}
	for name, mutate := range map[string]func(map[string]string){
		"短い署名鍵":       func(e map[string]string) { e["LIBREAD_SESSION_KEY"] = "c2hvcnQ=" },
		"base64 でない鍵": func(e map[string]string) { e["LIBREAD_SESSION_KEY"] = "!!!" },
		"http のオリジン":  func(e map[string]string) { e["LIBREAD_PUBLIC_ORIGIN"] = "http://libread.example" },
		"末尾に / のオリジン": func(e map[string]string) { e["LIBREAD_PUBLIC_ORIGIN"] = "https://libread.example/" },
	} {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			mutate(env)
			if _, _, err := loadConfig(func(k string) string { return env[k] }); err == nil {
				t.Error("want error")
			}
		})
	}
	// ローカルの開発用（quickstart.md 手順 3）。
	env := validEnv()
	env["LIBREAD_PUBLIC_ORIGIN"] = "http://localhost:5173"
	if _, _, err := loadConfig(func(k string) string { return env[k] }); err != nil {
		t.Errorf("http://localhost:5173: %v", err)
	}
}
