// Package proxy is GoGate's HTTP reverse-proxy half: one httputil.ReverseProxy
// per upstream, built on demand and reused. Routing, prefix stripping and the
// policy chain live in internal/server; this package only forwards.
package proxy

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// Pool hands out a shared *httputil.ReverseProxy per upstream base URL.
type Pool struct {
	mu        sync.Mutex
	byBase    map[string]*httputil.ReverseProxy
	transport http.RoundTripper
	log       *slog.Logger
	bufs      *bufferPool
}

// bufferPool reuses the response-copy buffers httputil.ReverseProxy would
// otherwise allocate per request (32 KiB each — the #1 allocator on the proxy
// path, see PROFILING.md / CoverGo U7).
type bufferPool struct{ p sync.Pool }

const copyBufSize = 32 * 1024

func newBufferPool() *bufferPool {
	return &bufferPool{p: sync.Pool{New: func() any {
		b := make([]byte, copyBufSize)
		return &b
	}}}
}

func (bp *bufferPool) Get() []byte  { return *bp.p.Get().(*[]byte) }
func (bp *bufferPool) Put(b []byte) { bp.p.Put(&b) }

// New returns a pool. A nil logger falls back to slog.Default; a nil transport
// uses a tuned default (bounded idle conns, sane timeouts).
func New(transport http.RoundTripper, log *slog.Logger) *Pool {
	if log == nil {
		log = slog.Default()
	}
	if transport == nil {
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}
	}
	return &Pool{byBase: map[string]*httputil.ReverseProxy{}, transport: transport, log: log, bufs: newBufferPool()}
}

// For returns the proxy for base ("scheme://host[:port]"), creating it once.
func (p *Pool) For(base string) (*httputil.ReverseProxy, error) {
	return p.ForRoute(base, 0)
}

// ForRoute is For with a per-route upstream response-header timeout (CoverGo
// P9). A non-zero timeout gets its own cloned Transport, cached separately, so
// a slow-backend route can be tightened without touching the others.
func (p *Pool) ForRoute(base string, respHeaderTimeout time.Duration) (*httputil.ReverseProxy, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("proxy: %q is not an absolute URL", base)
	}

	key := base
	if respHeaderTimeout > 0 {
		key = base + "|" + respHeaderTimeout.String()
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if rp, ok := p.byBase[key]; ok {
		return rp, nil
	}

	tr := p.transport
	if respHeaderTimeout > 0 {
		if bt, ok := p.transport.(*http.Transport); ok {
			clone := bt.Clone()
			clone.ResponseHeaderTimeout = respHeaderTimeout
			tr = clone
		}
	}

	target := &url.URL{Scheme: u.Scheme, Host: u.Host}
	rp := &httputil.ReverseProxy{
		Transport:  tr,
		BufferPool: p.bufs,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			// Present the upstream its own Host unless it opts into forwarding.
			pr.Out.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.log.Warn("proxy: upstream error", "base", base, "path", r.URL.Path, "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
		},
	}
	p.byBase[key] = rp
	return rp, nil
}
