package server_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

const secret = "s3cr3t"

func token(t *testing.T, sub string) string {
	t.Helper()
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	signing := enc(map[string]any{"alg": "HS256", "typ": "JWT"}) + "." +
		enc(map[string]any{"sub": sub, "exp": float64(time.Now().Add(time.Hour).Unix())})
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

type harness struct {
	gw       http.Handler
	routes   *route.MemStore
	upstream *httptest.Server
	hits     *int64
	lb       *bridge.Loopback
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	var hits int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("X-Seen-Path", r.URL.Path)
		w.Header().Set("X-Seen-Subject", r.Header.Get(auth.HeaderSubject))
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(up.Close)

	routes := route.NewMemStore()
	lb := bridge.NewLoopback()
	gw := server.New(server.Config{
		Routes:   routes,
		Verifier: auth.HS256{Secret: []byte(secret)},
		Proxy:    proxy.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Bridge:   lb,
		Cache:    cache.New(0),
		Limiter:  ratelimit.New(0),
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &harness{gw: gw, routes: routes, upstream: up, hits: &hits, lb: lb}
}

func (h *harness) do(method, target string, hdr http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = "gw.example"
	for k, vs := range hdr {
		req.Header[k] = vs
	}
	rec := httptest.NewRecorder()
	h.gw.ServeHTTP(rec, req)
	return rec
}

func TestNoRoute(t *testing.T) {
	h := newHarness(t)
	if rec := h.do("GET", "http://gw.example/nope", nil); rec.Code != 404 {
		t.Fatalf("no route = %d", rec.Code)
	}
}

func TestProxyAndStripPrefix(t *testing.T) {
	h := newHarness(t)
	h.routes.Add(route.Route{PathPrefix: "/api", StripPrefix: true, Target: route.Target{Upstream: h.upstream.URL}})

	rec := h.do("GET", "http://gw.example/api/users/7", nil)
	if rec.Code != 200 || rec.Header().Get("X-Seen-Path") != "/users/7" {
		t.Fatalf("proxy = %d, upstream saw path %q", rec.Code, rec.Header().Get("X-Seen-Path"))
	}
}

func TestRequireAuth(t *testing.T) {
	h := newHarness(t)
	h.routes.Add(route.Route{PathPrefix: "/", Target: route.Target{Upstream: h.upstream.URL},
		Policy: route.Policy{RequireAuth: true}})

	if rec := h.do("GET", "http://gw.example/x", nil); rec.Code != 401 {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}
	bad := http.Header{"Authorization": {"Bearer aaa.bbb.ccc"}}
	if rec := h.do("GET", "http://gw.example/x", bad); rec.Code != 401 {
		t.Fatalf("bad token = %d, want 401", rec.Code)
	}
	good := http.Header{"Authorization": {"Bearer " + token(t, "user-9")}}
	rec := h.do("GET", "http://gw.example/x", good)
	if rec.Code != 200 || rec.Header().Get("X-Seen-Subject") != "user-9" {
		t.Fatalf("good token = %d, upstream saw subject %q", rec.Code, rec.Header().Get("X-Seen-Subject"))
	}
}

func TestRateLimit(t *testing.T) {
	h := newHarness(t)
	h.routes.Add(route.Route{PathPrefix: "/", Target: route.Target{Upstream: h.upstream.URL},
		Policy: route.Policy{RateLimit: route.Rate{PerSecond: 1, Burst: 2}}})

	codes := []int{}
	for i := 0; i < 4; i++ {
		codes = append(codes, h.do("GET", "http://gw.example/x", nil).Code)
	}
	// burst 2 through, then 429s
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 || codes[3] != 429 {
		t.Fatalf("codes = %v, want [200 200 429 429]", codes)
	}
	if rec := h.do("GET", "http://gw.example/x", nil); rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 should carry Retry-After")
	}
}

func TestMaxInFlightReturns503(t *testing.T) {
	// A route capped at 1 in-flight, fed by a blockable upstream: the 2nd
	// concurrent request is turned away with 503 (CoverGo U18).
	reached := make(chan struct{}, 1)
	block := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached <- struct{}{}
		<-block
		_, _ = w.Write([]byte("ok"))
	}))
	defer up.Close()

	routes := route.NewMemStore()
	routes.Add(route.Route{PathPrefix: "/", Target: route.Target{Upstream: up.URL},
		Policy: route.Policy{MaxInFlight: 1}})
	gw := server.New(server.Config{
		Routes: routes, Proxy: proxy.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	call := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://gw.example/x", nil)
		req.Host = "gw.example"
		gw.ServeHTTP(rec, req)
		return rec
	}

	first := make(chan int, 1)
	go func() { first <- call().Code }()
	<-reached // the first request now holds the only slot, parked in the upstream

	rec := call() // second request — should be refused within the grace window
	if rec.Code != http.StatusServiceUnavailable {
		close(block)
		t.Fatalf("second concurrent request = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		close(block)
		t.Fatal("503 should carry Retry-After")
	}

	close(block)
	if code := <-first; code != 200 {
		t.Fatalf("first request = %d, want 200", code)
	}
}

func TestCacheHitAndCoalesce(t *testing.T) {
	h := newHarness(t)
	h.routes.Add(route.Route{PathPrefix: "/", Target: route.Target{Upstream: h.upstream.URL},
		Policy: route.Policy{CacheTTL: time.Minute}})

	r1 := h.do("GET", "http://gw.example/data", nil)
	if r1.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("first request X-Cache = %q, want MISS", r1.Header().Get("X-Cache"))
	}
	r2 := h.do("GET", "http://gw.example/data", nil)
	if r2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("second request X-Cache = %q, want HIT", r2.Header().Get("X-Cache"))
	}
	if got := atomic.LoadInt64(h.hits); got != 1 {
		t.Fatalf("upstream hit %d times, want 1 (cache served the rest)", got)
	}
}

func TestBridgeRoute(t *testing.T) {
	h := newHarness(t)
	h.lb.Handle("crm3-crm", func(_ context.Context, m bridge.Message) bridge.Reply {
		return bridge.Reply{Status: 202, Body: []byte("queued " + m.Path)}
	})
	h.routes.Add(route.Route{PathPrefix: "/crm", Target: route.Target{Subject: "crm3-crm"}})

	rec := h.do("POST", "http://gw.example/crm/contacts", nil)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), "/crm/contacts") {
		t.Fatalf("bridge route = %d %q", rec.Code, rec.Body.String())
	}
}

func TestAdminAPI(t *testing.T) {
	h := newHarness(t)

	if rec := h.do("GET", "http://gw.example/_gogate/healthz", nil); rec.Code != 200 {
		t.Fatalf("healthz = %d", rec.Code)
	}

	body := `{"path_prefix":"/api","target":{"upstream":"` + h.upstream.URL + `"}}`
	req := httptest.NewRequest("POST", "http://gw.example/_gogate/routes", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.gw.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("add route = %d: %s", rec.Code, rec.Body.String())
	}
	var added route.Route
	json.Unmarshal(rec.Body.Bytes(), &added)

	// invalid route → 422
	badReq := httptest.NewRequest("POST", "http://gw.example/_gogate/routes", strings.NewReader(`{"path_prefix":"nope"}`))
	badRec := httptest.NewRecorder()
	h.gw.ServeHTTP(badRec, badReq)
	if badRec.Code != 422 {
		t.Fatalf("invalid route = %d, want 422", badRec.Code)
	}

	// the added route works
	if rec := h.do("GET", "http://gw.example/api/x", nil); rec.Code != 200 {
		t.Fatalf("added route not serving: %d", rec.Code)
	}

	// delete it
	delReq := httptest.NewRequest("DELETE", "http://gw.example/_gogate/routes/"+added.ID, nil)
	delRec := httptest.NewRecorder()
	h.gw.ServeHTTP(delRec, delReq)
	if delRec.Code != 204 {
		t.Fatalf("delete route = %d", delRec.Code)
	}
	if rec := h.do("GET", "http://gw.example/api/x", nil); rec.Code != 404 {
		t.Fatalf("deleted route still serving: %d", rec.Code)
	}
}
