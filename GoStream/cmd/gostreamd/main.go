// Command gostreamd is the GoStream server: a WebSocket fan-out — clients
// subscribe to topics over /ws, anything POSTed to /pub/{topic} (or published
// by a permitted client) is delivered to that topic's subscribers, and a slow
// subscriber is dropped rather than waited on. Phase 0 (walking skeleton,
// in-memory hub, hand-rolled RFC 6455) — see ../../future.md.
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

	"github.com/levelcodingdev/gostream/internal/debug"
	"github.com/levelcodingdev/gostream/internal/hub"
	"github.com/levelcodingdev/gostream/internal/server"
)

var version = "0.0.0-dev"

func main() {
	addr := flag.String("addr", envOr("GOSTREAM_ADDR", ":8097"), "listen address")
	buffer := flag.Int("send-buffer", envInt("GOSTREAM_SEND_BUFFER", 64), "per-connection queued-message cap before drops")
	maxDropped := flag.Int("max-dropped", envInt("GOSTREAM_MAX_DROPPED", 32), "drops tolerated before a slow client is evicted")
	pubToken := flag.String("publish-token", os.Getenv("GOSTREAM_PUBLISH_TOKEN"), "token required to POST /pub (bearer or ?token=); empty = open")
	wsToken := flag.String("ws-token", os.Getenv("GOSTREAM_WS_TOKEN"), "token required to open /ws; empty = open")
	clientPublish := flag.Bool("client-publish", envBool("GOSTREAM_CLIENT_PUBLISH", false), "allow socket clients to publish, not just subscribe")
	idle := flag.Duration("idle-timeout", envDur("GOSTREAM_IDLE_TIMEOUT", 75*time.Second), "drop a socket that is silent this long")
	ping := flag.Duration("ping-interval", envDur("GOSTREAM_PING_INTERVAL", 30*time.Second), "server→client ping cadence")
	readLimit := flag.Int64("read-limit", int64(envInt("GOSTREAM_READ_LIMIT", 1<<20)), "max inbound message size in bytes")
	debugOpts := debug.Flags("GOSTREAM")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	debug.Start(debugOpts(), log)
	slog.SetDefault(log)
	server.Version = version

	h := hub.New(hub.Config{SendBuffer: *buffer, MaxDropped: *maxDropped})
	handler := server.New(server.Config{
		Hub:                h,
		PublishToken:       *pubToken,
		WSToken:            *wsToken,
		AllowClientPublish: *clientPublish,
		IdleTimeout:        *idle,
		PingInterval:       *ping,
		ReadLimit:          *readLimit,
		Log:                log,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Info("gostreamd listening", "addr", *addr,
			"send_buffer", *buffer, "max_dropped", *maxDropped, "client_publish", *clientPublish)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
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
