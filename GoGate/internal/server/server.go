// Package server assembles GoGate: an admin API under /_gogate and, for every
// other request, the policy chain — match a route, verify the bearer token,
// rate-limit, (optionally) serve from cache with request coalescing, then
// reverse-proxy to an HTTP upstream or bridge to a queue subject.
package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/levelcodingdev/gogate/internal/auth"
	"github.com/levelcodingdev/gogate/internal/bridge"
	"github.com/levelcodingdev/gogate/internal/cache"
	"github.com/levelcodingdev/gogate/internal/inflight"
	"github.com/levelcodingdev/gogate/internal/proxy"
	"github.com/levelcodingdev/gogate/internal/ratelimit"
	"github.com/levelcodingdev/gogate/internal/route"
	"github.com/levelcodingdev/gogate/internal/uid"
)

// Version is stamped by main.
var Version = "dev"

// Config wires the gateway.
type Config struct {
	Routes      route.Store
	Verifier    auth.Verifier // nil → RequireAuth routes always 401
	CookieName  string        // also accept the token from this cookie
	Proxy       *proxy.Pool
	Bridge      bridge.Transport   // nil → bridge routes 502
	Cache       *cache.Cache       // nil → no caching
	Limiter     *ratelimit.Limiter // nil → no rate limiting
	Log         *slog.Logger
	AdminPrefix string // default "/_gogate"
}

type server struct {
	cfg      Config
	admin    http.Handler
	inflight *inflight.Limiter
}

// New returns the gateway handler.
func New(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.AdminPrefix == "" {
		cfg.AdminPrefix = "/_gogate"
	}
	s := &server{cfg: cfg, inflight: inflight.New(0)}
	s.admin = s.adminMux()
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rid := r.Header.Get("X-Request-Id")
	if rid == "" {
		rid = uid.New()[:12]
	}
	w.Header().Set("X-Request-Id", rid)

	defer func() {
		if rec := recover(); rec != nil {
			s.cfg.Log.Error("gogate: panic", "rid", rid, "path", r.URL.Path, "recover", rec)
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
	}()

	if strings.HasPrefix(r.URL.Path, s.cfg.AdminPrefix) {
		s.admin.ServeHTTP(w, r)
		return
	}
	s.route(w, r, rid)
}

func (s *server) route(w http.ResponseWriter, r *http.Request, rid string) {
	rt, ok := s.cfg.Routes.Match(r.Host, r.URL.Path)
	if !ok {
		writeErr(w, http.StatusNotFound, "no route")
		return
	}

	// Verify a bearer/cookie token if one is present. A present-but-invalid
	// token is always rejected; a missing one is only fatal on RequireAuth.
	var claims auth.Claims
	authed := false
	if s.cfg.Verifier != nil {
		c, ok, err := auth.Resolve(s.cfg.Verifier, r, s.cfg.CookieName)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		claims, authed = c, ok
	}

	// Rate limit on the identity we have: subject if authed, else client IP.
	if s.cfg.Limiter != nil {
		key := clientIP(r)
		if authed && claims.Subject != "" {
			key = "sub:" + claims.Subject
		}
		if allowed, retry := s.cfg.Limiter.Allow(key, rt.Policy.RateLimit); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			writeErr(w, http.StatusTooManyRequests, "rate limited")
			return
		}
	}

	if rt.Policy.RequireAuth && !authed {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Cap concurrent in-flight requests to this route's upstream (CoverGo U18).
	if rt.Policy.MaxInFlight > 0 {
		release, ok := s.inflight.Acquire(r.Context(), rt.ID, rt.Policy.MaxInFlight)
		if !ok {
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusServiceUnavailable, "upstream at capacity")
			return
		}
		defer release()
	}

	s.forward(w, r, rt, claims, authed)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":` + strconv.Quote(msg) + `}`))
}
