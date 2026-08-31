// Package edge is the seam for GoFlare's other half: a Cloudflare-style edge in
// front of an app that also captures that app's failures as error events. This
// Phase 0 version is deliberately thin — a host-routed reverse proxy that turns
// upstream 5xx and connection failures into synthetic events. WAF rules,
// response caching, DNS and Workers are later phases (see future.md §3); the
// point here is that the capture path is real and wired to the same grouping
// store the SDK ingest path uses.
package edge

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
)

// Capturer is the slice of the grouping store the edge needs.
type Capturer interface {
	Ingest(projectID string, e event.Event) (group.Issue, group.Outcome)
}

// Route binds an inbound host to an upstream and the GoFlare project that
// upstream's failures are filed under.
type Route struct {
	Host      string // matched against the request Host header; "" matches any
	Upstream  string // scheme://host[:port]
	ProjectID string
}

// Proxy is the host-routed reverse proxy.
type Proxy struct {
	capture Capturer
	log     *slog.Logger

	mu     sync.RWMutex
	routes []compiledRoute
}

type compiledRoute struct {
	host      string
	projectID string
	rp        *httputil.ReverseProxy
	target    *url.URL
}

// New builds an empty Proxy. Add routes with SetRoutes.
func New(capture Capturer, log *slog.Logger) *Proxy {
	if log == nil {
		log = slog.Default()
	}
	return &Proxy{capture: capture, log: log}
}

// SetRoutes replaces the routing table.
func (p *Proxy) SetRoutes(routes []Route) error {
	compiled := make([]compiledRoute, 0, len(routes))
	for _, r := range routes {
		u, err := url.Parse(r.Upstream)
		if err != nil || u.Host == "" {
			return fmt.Errorf("edge: bad upstream %q", r.Upstream)
		}
		cr := compiledRoute{host: strings.ToLower(r.Host), projectID: r.ProjectID, target: u}
		cr.rp = &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(u)
				pr.SetXForwarded()
				pr.Out.Host = u.Host
			},
			ModifyResponse: p.responseHook(cr),
			ErrorHandler:   p.errorHook(cr),
		}
		compiled = append(compiled, cr)
	}
	p.mu.Lock()
	p.routes = compiled
	p.mu.Unlock()
	return nil
}

// ServeHTTP routes by Host and proxies, capturing failures.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cr, ok := p.match(r.Host)
	if !ok {
		http.Error(w, "no edge route for host "+r.Host, http.StatusBadGateway)
		return
	}
	cr.rp.ServeHTTP(w, r)
}

func (p *Proxy) match(host string) (compiledRoute, bool) {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	var wildcard *compiledRoute
	for i := range p.routes {
		switch p.routes[i].host {
		case host:
			return p.routes[i], true
		case "":
			wildcard = &p.routes[i]
		}
	}
	if wildcard != nil {
		return *wildcard, true
	}
	return compiledRoute{}, false
}
