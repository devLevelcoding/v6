// Command gouptimed is the GoUptime server: a monitoring engine that probes
// HTTP and TCP targets on their intervals, detects incidents with a hysteresis
// policy, and dispatches incident events to a webhook and the log. Phase 0
// (walking skeleton, in-memory stores) — see ../../future.md.
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

	"github.com/levelcodingdev/gouptime/internal/check"
	"github.com/levelcodingdev/gouptime/internal/history"
	"github.com/levelcodingdev/gouptime/internal/incident"
	"github.com/levelcodingdev/gouptime/internal/monitor"
	"github.com/levelcodingdev/gouptime/internal/notify"
	"github.com/levelcodingdev/gouptime/internal/scheduler"
	"github.com/levelcodingdev/gouptime/internal/server"
)

func main() {
	addr := flag.String("addr", envOr("GOUPTIME_ADDR", ":8095"), "listen address")
	webhookURL := flag.String("webhook", os.Getenv("GOUPTIME_WEBHOOK_URL"), "incident webhook URL (empty = log only)")
	failN := flag.Int("fail-threshold", envInt("GOUPTIME_FAIL_THRESHOLD", 3), "consecutive failures before an incident opens")
	recoverN := flag.Int("recover-threshold", envInt("GOUPTIME_RECOVER_THRESHOLD", 2), "consecutive successes before an incident resolves")
	retain := flag.Int("retain", envInt("GOUPTIME_RETAIN", 500), "results kept in memory per monitor")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	store := monitor.NewMemStore()
	prober := check.NewProber()
	detector := incident.NewDetector(incident.Policy{FailThreshold: *failN, RecoverThreshold: *recoverN})
	ring := history.NewRing(*retain)

	notifier := notify.Multi{
		notify.LogNotifier{Logger: log},
		notify.WebhookNotifier{URL: *webhookURL},
	}
	if *webhookURL != "" {
		log.Info("webhook notifications enabled", "url", *webhookURL)
	} else {
		log.Info("webhook URL not set — incidents go to the log only (pass -webhook or GOUPTIME_WEBHOOK_URL)")
	}

	sched := scheduler.New(store, prober, detector, ring, notifier, log)
	srv := server.New(store, sched, detector, ring)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sched.Start(ctx)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("gouptimed listening", "addr", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Error("http shutdown", "err", err)
	}
	sched.Wait()
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
