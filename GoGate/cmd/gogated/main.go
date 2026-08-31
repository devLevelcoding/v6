// Command gogated is the GoGate server: an edge gateway / BFF in front of one
// or more apps — route matching, JWT verification, token-bucket rate limiting,
// a coalescing TTL response cache, and an HTTP↔queue bridge, all in one static
// binary. Phase 0 (walking skeleton, in-memory route table + loopback bridge) —
// see ../../future.md.
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

	"github.com/levelcodingdev/gogate/internal/auth"
	"github.com/levelcodingdev/gogate/internal/bridge"
	"github.com/levelcodingdev/gogate/internal/cache"
	"github.com/levelcodingdev/gogate/internal/debug"
	"github.com/levelcodingdev/gogate/internal/proxy"
	"github.com/levelcodingdev/gogate/internal/ratelimit"
	"github.com/levelcodingdev/gogate/internal/route"
	"github.com/levelcodingdev/gogate/internal/server"
	"github.com/levelcodingdev/gogate/internal/tlsconf"
)

var version = "0.0.0-dev"

func main() {
	addr := flag.String("addr", envOr("GOGATE_ADDR", ":8090"), "listen address")
	jwtSecret := flag.String("jwt-secret", os.Getenv("GOGATE_JWT_SECRET"), "HS256 secret for verifying bearer tokens (empty = no verification, RequireAuth routes 401)")
	jwtCookie := flag.String("jwt-cookie", os.Getenv("GOGATE_JWT_COOKIE"), "also read the token from this cookie")
	configPath := flag.String("config", os.Getenv("GOGATE_CONFIG"), "JSON file: an array of routes to load at boot")
	upstream := flag.String("upstream", os.Getenv("GOGATE_UPSTREAM"), "shorthand: proxy everything (prefix /) to this URL")
	upstreamAuth := flag.Bool("upstream-auth", envBool("GOGATE_UPSTREAM_AUTH", false), "require a valid token for the -upstream route")
	cacheTTL := flag.Duration("upstream-cache", envDur("GOGATE_UPSTREAM_CACHE", 0), "cache GET/HEAD on the -upstream route for this long (0 = off)")
	cacheMax := flag.Int("cache-entries", envInt("GOGATE_CACHE_ENTRIES", 4096), "max cached responses")
	tlsCert := flag.String("tls-cert", os.Getenv("GOGATE_TLS_CERT"), "server certificate PEM (enables HTTPS)")
	tlsKey := flag.String("tls-key", os.Getenv("GOGATE_TLS_KEY"), "server private key PEM")
	tlsClientCA := flag.String("tls-client-ca", os.Getenv("GOGATE_TLS_CLIENT_CA"), "require client certs signed by this CA (inbound mTLS)")
	upstreamCA := flag.String("upstream-ca", os.Getenv("GOGATE_UPSTREAM_CA"), "pin upstream TLS to this CA")
	upstreamCert := flag.String("upstream-cert", os.Getenv("GOGATE_UPSTREAM_CERT"), "client cert to present to upstreams (outbound mTLS)")
	upstreamKey := flag.String("upstream-key", os.Getenv("GOGATE_UPSTREAM_KEY"), "client key for outbound mTLS")
	debugOpts := debug.Flags("GOGATE")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	server.Version = version

	debug.Start(debugOpts(), log)

	routes := route.NewMemStore()
	if *configPath != "" {
		raw, err := os.ReadFile(*configPath)
		if err != nil {
			log.Error("read config", "path", *configPath, "err", err)
			os.Exit(1)
		}
		n, err := server.LoadRoutes(routes, raw)
		if err != nil {
			log.Error("load config", "route_index", n, "err", err)
			os.Exit(1)
		}
		log.Info("routes loaded from config", "count", n, "path", *configPath)
	}
	if *upstream != "" {
		if _, err := routes.Add(route.Route{
			PathPrefix: "/",
			Target:     route.Target{Upstream: *upstream},
			Policy:     route.Policy{RequireAuth: *upstreamAuth, CacheTTL: *cacheTTL},
		}); err != nil {
			log.Error("upstream route", "err", err)
			os.Exit(1)
		}
		log.Info("proxying / to upstream", "upstream", *upstream, "auth", *upstreamAuth, "cache", cacheTTL.String())
	}

	var upstreamTransport http.RoundTripper
	if *upstreamCA != "" || *upstreamCert != "" || *upstreamKey != "" {
		utc, err := tlsconf.Upstream(*upstreamCA, *upstreamCert, *upstreamKey)
		if err != nil {
			log.Error("upstream TLS", "err", err)
			os.Exit(1)
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = utc
		upstreamTransport = tr
		log.Info("upstream TLS configured", "ca_pinned", *upstreamCA != "", "mtls", *upstreamCert != "")
	}

	cfg := server.Config{
		Routes:  routes,
		Proxy:   proxy.New(upstreamTransport, log),
		Bridge:  bridge.NewLoopback(), // Phase 0: in-process; a broker Transport is a later phase
		Cache:   cache.New(*cacheMax),
		Limiter: ratelimit.New(0),
		Log:     log,
	}
	if *jwtSecret != "" {
		cfg.Verifier = auth.HS256{Secret: []byte(*jwtSecret)}
		cfg.CookieName = *jwtCookie
		log.Info("JWT verification enabled (HS256)")
	} else {
		log.Info("JWT verification disabled — set -jwt-secret to enable")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{Addr: *addr, Handler: server.New(cfg), ReadHeaderTimeout: 10 * time.Second}
	serve := httpSrv.ListenAndServe
	scheme := "http"
	if *tlsCert != "" || *tlsKey != "" {
		tc, err := tlsconf.Server(*tlsCert, *tlsKey, *tlsClientCA)
		if err != nil {
			log.Error("server TLS", "err", err)
			os.Exit(1)
		}
		httpSrv.TLSConfig = tc
		serve = func() error { return httpSrv.ListenAndServeTLS("", "") }
		scheme = "https"
		if *tlsClientCA != "" {
			scheme += " (mTLS)"
		}
	}
	go func() {
		log.Info("gogated listening", "addr", *addr, "scheme", scheme, "admin", "/_gogate")
		if err := serve(); err != nil && err != http.ErrServerClosed {
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
