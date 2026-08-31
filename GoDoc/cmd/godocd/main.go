// Command godocd is the GoDoc server: a stateless document / export service.
// POST a spec + data to /v1/csv, /v1/xml or /v1/pdf (or /v1/render with the
// format in the body) and it streams back the file. Nothing is stored; every
// instance is interchangeable. Phase 0 — see ../../future.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/levelcodingdev/godoc/internal/server"
)

var version = "0.0.0-dev"

func main() {
	addr := flag.String("addr", envOr("GODOC_ADDR", ":8098"), "listen address")
	token := flag.String("token", os.Getenv("GODOC_TOKEN"), "bearer token required for /v1/* (empty = open)")
	maxBody := flag.Int64("max-body", int64(envInt("GODOC_MAX_BODY", 8<<20)), "max request body in bytes")
	timeout := flag.Duration("timeout", envDur("GODOC_TIMEOUT", 30*time.Second), "per-request render deadline")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	server.Version = version

	handler := server.New(server.Config{
		Token:   *token,
		MaxBody: *maxBody,
		Timeout: *timeout,
		Log:     log,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Info("godocd listening", "addr", *addr, "auth", *token != "")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
