// Command libread-server は libread の Web サーバー（spec 002）。環境変数で設定する（quickstart.md 手順 3・4）。
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // コンテナに tzdata がなくても Asia/Tokyo を使えるようにする

	"libread/internal/auth"
	"libread/internal/server"
)

// minSessionKeyBytes はセッションの署名鍵の最小の長さ（HMAC-SHA256 の出力と同じ32バイト）。
const minSessionKeyBytes = 32

func main() {
	addr, cfg, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "libread-server: %v\n", err)
		os.Exit(2)
	}
	if err := run(addr, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "libread-server: %v\n", err)
		os.Exit(1)
	}
}

// loadConfig は環境変数からサーバーの設定を作る。LIBREAD_CALIL_APPKEY 以外は必須。
func loadConfig(getenv func(string) string) (string, server.Config, error) {
	required := []string{
		"LIBREAD_DATA_DIR",
		"LIBREAD_CACHE_DIR",
		"LIBREAD_STATIC_DIR",
		"LIBREAD_PUBLIC_ORIGIN",
		"LIBREAD_OWNER_EMAIL",
		"LIBREAD_GOOGLE_CLIENT_ID",
		"LIBREAD_GOOGLE_CLIENT_SECRET",
		"LIBREAD_SESSION_KEY",
	}
	var missing []string
	for _, name := range required {
		if getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return "", server.Config{}, fmt.Errorf("環境変数 %s を設定してください（quickstart.md 手順 3）", strings.Join(missing, "・"))
	}

	key, err := base64.StdEncoding.DecodeString(getenv("LIBREAD_SESSION_KEY"))
	if err != nil || len(key) < minSessionKeyBytes {
		return "", server.Config{}, fmt.Errorf("LIBREAD_SESSION_KEY は %d バイト以上を base64 にした値にしてください（例: openssl rand -base64 32）", minSessionKeyBytes)
	}
	origin := getenv("LIBREAD_PUBLIC_ORIGIN")
	if !strings.HasPrefix(origin, "https://") && !strings.HasPrefix(origin, "http://localhost:") {
		return "", server.Config{}, errors.New("LIBREAD_PUBLIC_ORIGIN は https:// で始まるオリジン（ローカルでは http://localhost:<ポート>）にしてください")
	}
	if strings.HasSuffix(origin, "/") {
		return "", server.Config{}, errors.New("LIBREAD_PUBLIC_ORIGIN の末尾に / を付けないでください")
	}

	port := getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	cfg := server.Config{
		DataDir:  getenv("LIBREAD_DATA_DIR"),
		CacheDir: getenv("LIBREAD_CACHE_DIR"),
		Static:   os.DirFS(getenv("LIBREAD_STATIC_DIR")),
		Auth: auth.Config{
			ClientID:     getenv("LIBREAD_GOOGLE_CLIENT_ID"),
			ClientSecret: getenv("LIBREAD_GOOGLE_CLIENT_SECRET"),
			PublicOrigin: origin,
			OwnerEmail:   getenv("LIBREAD_OWNER_EMAIL"),
			HTTP:         httpClient,
		},
		SessionKey:  key,
		CalilAppKey: getenv("LIBREAD_CALIL_APPKEY"),
		Now:         time.Now,
		HTTP:        httpClient,
		Log:         os.Stdout,
	}
	return ":" + port, cfg, nil
}

// run はサーバーを起動し、SIGTERM（Cloud Run の停止）・SIGINT で処理中の要求を終えてから止まる。
func run(addr string, cfg server.Config) error {
	if cfg.CalilAppKey == "" {
		fmt.Fprintln(os.Stderr, "libread-server: 警告: LIBREAD_CALIL_APPKEY が設定されていません。カーリルを使う API は 503 を返します")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	fmt.Fprintf(os.Stdout, "libread-server: %s で待ち受けます\n", addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("停止できません: %w", err)
	}
	return nil
}
