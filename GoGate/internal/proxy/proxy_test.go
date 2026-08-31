package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestForReusesAndValidates(t *testing.T) {
	p := New(nil, quiet())
	a, err := p.For("http://example.com:8080")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.For("http://example.com:8080")
	if a != b {
		t.Fatal("For should return the same proxy for the same base")
	}
	if _, err := p.For("not-a-url"); err == nil {
		t.Fatal("For should reject a relative URL")
	}
}

func TestProxyForwards(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") == "" || r.Header.Get("X-Forwarded-Proto") == "" {
			t.Errorf("missing X-Forwarded-* headers: %v", r.Header)
		}
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(207)
		_, _ = w.Write([]byte("from upstream " + r.URL.Path))
	}))
	defer upstream.Close()

	rp, err := New(nil, quiet()).For(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest("GET", "http://gw.example/thing", nil))

	if rec.Code != 207 || rec.Header().Get("X-Upstream") != "yes" || rec.Body.String() != "from upstream /thing" {
		t.Fatalf("proxied response = %d %q %q", rec.Code, rec.Header().Get("X-Upstream"), rec.Body.String())
	}
}

func TestProxyUpstreamDown(t *testing.T) {
	rp, _ := New(nil, quiet()).For("http://127.0.0.1:1") // nothing listens on port 1
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest("GET", "http://gw/x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("dead upstream → %d, want 502", rec.Code)
	}
}

// CoverGo P9: a per-route timeout gets its own cached proxy/Transport.
func TestForRoutePerRouteTransport(t *testing.T) {
	p := New(nil, nil)
	a, err := p.ForRoute("http://up.example", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.ForRoute("http://up.example", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("a timed route should get a distinct proxy instance")
	}
	again, _ := p.ForRoute("http://up.example", 2*time.Second)
	if again != b {
		t.Fatal("same base+timeout should return the cached instance")
	}
	tr, ok := b.Transport.(*http.Transport)
	if !ok || tr.ResponseHeaderTimeout != 2*time.Second {
		t.Fatalf("cloned transport timeout = %v", tr.ResponseHeaderTimeout)
	}
}
