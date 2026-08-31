// Command gorenderd is the GoRender server: submit a render Spec over HTTP, a
// pool of workers compiles it to one ffmpeg filtergraph and runs it, progress
// streams back over SSE, and the finished MP4 is downloadable. Phase 0 (walking
// skeleton, in-memory job store + queue) — see ../../future.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/levelcodingdev/gorender/internal/debug"
	"github.com/levelcodingdev/gorender/internal/events"
	"github.com/levelcodingdev/gorender/internal/job"
	"github.com/levelcodingdev/gorender/internal/media"
	"github.com/levelcodingdev/gorender/internal/queue"
	"github.com/levelcodingdev/gorender/internal/server"
	"github.com/levelcodingdev/gorender/internal/worker"
)

var version = "0.0.0-dev"

func main() {
	addr := flag.String("addr", envOr("GORENDER_ADDR", ":8096"), "listen address")
	outDir := flag.String("out", envOr("GORENDER_OUT_DIR", "./out"), "directory for finished renders")
	// GOMAXPROCS(0) is cgroup-aware on go 1.25+ (unlike NumCPU) — so a container
	// with a 2-CPU quota on a 32-core host sizes the pool to 2, not 32 (U6).
	workers := flag.Int("workers", envInt("GORENDER_WORKERS", runtime.GOMAXPROCS(0)), "concurrent ffmpeg jobs")
	queueCap := flag.Int("queue", envInt("GORENDER_QUEUE", 128), "max jobs waiting before submit is rejected")
	jobTimeout := flag.Duration("job-timeout", envDur("GORENDER_JOB_TIMEOUT", 30*time.Minute), "hard cap on one render (0 = none)")
	ffmpegBin := flag.String("ffmpeg", os.Getenv("GORENDER_FFMPEG"), "path to ffmpeg (default: PATH)")
	ffprobeBin := flag.String("ffprobe", os.Getenv("GORENDER_FFPROBE"), "path to ffprobe (default: PATH)")
	debugOpts := debug.Flags("GORENDER")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	debug.Start(debugOpts(), log)
	server.Version = version

	tools, err := media.Locate(*ffmpegBin, *ffprobeBin)
	if err != nil {
		log.Error("ffmpeg toolchain not found — install ffmpeg or pass -ffmpeg/-ffprobe", "err", err)
		os.Exit(1)
	}
	log.Info("ffmpeg toolchain", "ffmpeg", tools.FFmpeg, "ffprobe", tools.FFprobe)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Error("cannot create output dir", "dir", *outDir, "err", err)
		os.Exit(1)
	}

	store := job.NewStore()
	q := queue.NewMem(*queueCap)
	broker := events.NewBroker()

	pool := &worker.Pool{
		N:          *workers,
		Queue:      q,
		Store:      store,
		Encoder:    media.FFmpegEncoder{Bin: tools.FFmpeg, Log: log},
		Prober:     tools,
		Events:     broker,
		OutDir:     *outDir,
		Log:        log,
		JobTimeout: *jobTimeout,
	}

	handler := server.New(server.Deps{
		Store:   store,
		Queue:   q,
		Events:  broker,
		OutDir:  *outDir,
		FFmpeg:  tools.FFmpeg,
		FFprobe: tools.FFprobe,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool.Start(ctx)

	httpSrv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Info("gorenderd listening", "addr", *addr, "workers", *workers)
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
	pool.Wait()
	log.Info("stopped")
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
