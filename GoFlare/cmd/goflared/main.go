// Command goflared is the GoFlare server: a Sentry-style error-tracking core
// (SDK ingest → fingerprint grouping → issue API) with the seam for a
// Cloudflare-style edge that captures an upstream app's failures as events.
//
// Storage: with no -database-url it runs in memory and persists to a JSON
// snapshot (Phase 0). With -database-url it uses Postgres for projects, issues
// and an event index, and a blob store (local dir or S3) for raw event bodies,
// and ingest becomes an async queue with backpressure (Phase 1). Storage wiring
// is in storage.go; env/flag helpers in env.go. See ../../future.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/levelcodingdev/goflare/internal/debug"
	"github.com/levelcodingdev/goflare/internal/edge"
	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/ingest"
	"github.com/levelcodingdev/goflare/internal/server"
)

func main() {
	addr := flag.String("addr", envOr("GOFLARE_ADDR", ":9000"), "listen address for ingest + dashboard API")
	edgeAddr := flag.String("edge-addr", envOr("GOFLARE_EDGE_ADDR", ":9001"), "listen address for the edge proxy")
	publicURL := flag.String("public-url", envOr("GOFLARE_PUBLIC_URL", "http://localhost:9000"), "externally reachable base URL, used to render project DSNs")
	perIssue := flag.Int("events-per-issue", envInt("GOFLARE_EVENTS_PER_ISSUE", 50), "events sampled per issue in the in-memory store (Postgres keeps them all)")
	edgeUpstream := flag.String("edge-upstream", os.Getenv("GOFLARE_EDGE_UPSTREAM"), "upstream URL to proxy at the edge (enables the edge listener)")
	edgeHost := flag.String("edge-host", os.Getenv("GOFLARE_EDGE_HOST"), "Host header the edge route matches (empty = any)")
	edgeProject := flag.String("edge-project", os.Getenv("GOFLARE_EDGE_PROJECT"), "project slug or id that edge failures are filed under")
	seedProject := flag.String("seed-project", os.Getenv("GOFLARE_SEED_PROJECT"), "create a project with this name on boot and log its DSN")
	seedProjectID := flag.String("seed-project-id", os.Getenv("GOFLARE_SEED_PROJECT_ID"), "fixed id for the seeded project (empty = generated)")
	seedKey := flag.String("seed-key", os.Getenv("GOFLARE_SEED_KEY"), "fixed DSN public key for the seeded project (empty = generated)")
	snapEvery := flag.Duration("snapshot-interval", 15*time.Second, "how often to flush the snapshot file when state changed")

	cfg := storageFlags()
	ingestWorkers := flag.Int("ingest-workers", envInt("GOFLARE_INGEST_WORKERS", 4), "goroutines draining the ingest queue")
	ingestQueue := flag.Int("ingest-queue", envInt("GOFLARE_INGEST_QUEUE", 2048), "events buffered before ingest returns 429 (0 = group synchronously in the request)")
	debugOpts := debug.Flags("GOFLARE")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	debug.Start(debugOpts(), log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	groups := group.NewStore(*perIssue)
	st := openStorage(ctx, log, *cfg, groups)

	if *seedProject != "" {
		_, existed := st.projects.BySlug(slugOf(*seedProject))
		p, err := seedProjectInto(st.projects, *seedProject, *seedProjectID, *seedKey)
		switch {
		case err != nil:
			log.Error("seed project", "err", err)
		case existed == nil:
			log.Info("seed project already present", "slug", p.Slug, "id", p.ID, "dsn", p.DSN(*publicURL))
		default:
			log.Info("seeded project", "slug", p.Slug, "id", p.ID, "dsn", p.DSN(*publicURL))
			st.touch()
		}
	}

	// Async ingest pipeline (Phase 1). Queue 0 keeps the Phase-0 synchronous path.
	var pipe *ingest.Pipeline
	if *ingestQueue > 0 {
		pipe = ingest.NewPipeline(groups, *ingestWorkers, *ingestQueue, log)
		pipe.Start(ctx)
	}

	core := &http.Server{
		Addr:              *addr,
		Handler:           server.New(st.projects, groups, *publicURL, pipe, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	stopSnap := make(chan struct{})
	if st.snap != nil {
		go st.snap.Run(stopSnap, *snapEvery, st.memProj, groups)
		log.Info("snapshot enabled", "path", cfg.snapPath, "interval", snapEvery.String())
	}

	go serve(core, "core", func() { log.Info("goflared core listening", "addr", *addr, "public_url", *publicURL) }, log, stop)

	var edgeSrv *http.Server
	if *edgeUpstream != "" {
		pid, err := resolveProject(st.projects, *edgeProject)
		if err != nil {
			log.Error("edge-project", "err", err)
			stop()
		} else {
			proxy := edge.New(groups, log)
			if err := proxy.SetRoutes([]edge.Route{{Host: *edgeHost, Upstream: *edgeUpstream, ProjectID: pid}}); err != nil {
				log.Error("edge routes", "err", err)
				stop()
			} else {
				edgeSrv = &http.Server{Addr: *edgeAddr, Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
				go serve(edgeSrv, "edge", func() {
					log.Info("goflared edge listening", "addr", *edgeAddr, "upstream", *edgeUpstream, "project", pid)
				}, log, stop)
			}
		}
	}

	<-ctx.Done()
	log.Info("shutting down")

	if pipe != nil {
		if err := pipe.Wait(); err != nil { // ctx cancelled — workers drain the queue then exit
			log.Warn("ingest pipeline exited with error", "err", err)
		}
		log.Info("ingest pipeline drained")
	}
	if st.snap != nil {
		close(stopSnap)
		if err := st.snap.Save(st.memProj, groups); err != nil {
			log.Error("snapshot final save", "err", err)
		} else {
			log.Info("snapshot saved on shutdown", "path", cfg.snapPath)
		}
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = core.Shutdown(shutCtx)
	if edgeSrv != nil {
		_ = edgeSrv.Shutdown(shutCtx)
	}
}

// serve runs srv.ListenAndServe, logging its start and stopping the process on
// an unexpected error.
func serve(srv *http.Server, name string, onStart func(), log *slog.Logger, stop func()) {
	onStart()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error(name+" http", "err", err)
		stop()
	}
}
