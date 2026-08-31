package server_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/levelcodingdev/gogate/internal/auth"
	"github.com/levelcodingdev/gogate/internal/bridge"
	"github.com/levelcodingdev/gogate/internal/cache"
	"github.com/levelcodingdev/gogate/internal/proxy"
	"github.com/levelcodingdev/gogate/internal/ratelimit"
	"github.com/levelcodingdev/gogate/internal/route"
	"github.com/levelcodingdev/gogate/internal/server"
)

// CoverGo U1 — the full gateway handler cost (match → policy → proxy) against a
// local upstream. This is the path featureGo.md claims 6k→90k rps on; run
// `benchstat old.txt new.txt` before/after any change to the hot path.

func benchGateway(b *testing.B, r route.Route) http.Handler {
	b.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public")
		_, _ = w.Write([]byte("ok"))
	}))
	b.Cleanup(up.Close)

	routes := route.NewMemStore()
	if r.Target.Upstream == "" && r.Target.Subject == "" {
		r.Target.Upstream = up.URL
	}
	if _, err := routes.Add(r); err != nil {
		b.Fatalf("add route: %v", err)
	}
	return server.New(server.Config{
		Routes:   routes,
		Verifier: auth.HS256{Secret: []byte(secret)},
		Proxy:    proxy.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Bridge:   bridge.NewLoopback(),
		Cache:    cache.New(0),
		Limiter:  ratelimit.New(0),
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func drive(b *testing.B, gw http.Handler, hdr http.Header) {
	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest("GET", "http://gw.example/api/thing", nil)
		req.Host = "gw.example"
		for k, v := range hdr {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status %d", rec.Code)
		}
	}
}

func BenchmarkGatewayProxyPlain(b *testing.B) {
	drive(b, benchGateway(b, route.Route{PathPrefix: "/api", StripPrefix: true}), nil)
}

func BenchmarkGatewayProxyAuth(b *testing.B) {
	gw := benchGateway(b, route.Route{
		PathPrefix: "/api", StripPrefix: true,
		Policy: route.Policy{RequireAuth: true},
	})
	drive(b, gw, http.Header{"Authorization": {"Bearer " + benchToken("bench-user")}})
}

func BenchmarkGatewayProxyRateLimited(b *testing.B) {
	gw := benchGateway(b, route.Route{
		PathPrefix: "/api", StripPrefix: true,
		Policy: route.Policy{RateLimit: route.Rate{PerSecond: 1e9, Burst: 1e9}},
	})
	drive(b, gw, nil)
}

func BenchmarkGatewayCacheHit(b *testing.B) {
	gw := benchGateway(b, route.Route{
		PathPrefix: "/api", StripPrefix: true,
		Policy: route.Policy{CacheTTL: time.Hour},
	})
	req := httptest.NewRequest("GET", "http://gw.example/api/thing", nil)
	req.Host = "gw.example"
	gw.ServeHTTP(httptest.NewRecorder(), req) // prime
	drive(b, gw, nil)
}

// benchToken mints a valid HS256 JWT without a *testing.T (server_test.token needs one).
func benchToken(sub string) string {
	return signHS256(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{"sub": sub, "exp": float64(time.Now().Add(time.Hour).Unix())},
	)
}

func signHS256(header, claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(header) + "." + enc(claims)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
