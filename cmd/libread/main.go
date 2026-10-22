// Command libread は、読みかけの本を今日どの図書館で読めるかを表示する。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"libread/internal/cli"
	"libread/internal/store"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dataDir, err := store.ResolveDataDir(os.Getenv("LIBREAD_DATA_DIR"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "libread: %v\n", err)
		return cli.ExitError
	}
	cacheDir, err := store.ResolveCacheDir(os.Getenv("LIBREAD_CACHE_DIR"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "libread: %v\n", err)
		return cli.ExitError
	}

	return cli.Run(ctx, os.Args[1:], cli.Env{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Now:         time.Now,
		HTTPClient:  &http.Client{Timeout: 10 * time.Second},
		DataDir:     dataDir,
		CacheDir:    cacheDir,
		CalilAppKey: os.Getenv("LIBREAD_CALIL_APPKEY"),
	})
}
