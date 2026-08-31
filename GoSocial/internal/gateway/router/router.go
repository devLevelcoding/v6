// Package router is a reverse-proxying gateway: it dispatches requests to
// upstreams by path prefix, running each route's configured plugin chain
// first (see internal/gateway/plugin).
package router

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"gosocial/internal/gateway/plugin"
)

// RouteConfig defines a single proxied route.
type RouteConfig struct {
	Prefix      string
	Upstream    string
	Plugins     []string
	StripPrefix bool
}

// Router holds all proxied routes and their plugin chains.
type Router struct {
	mux      *http.ServeMux
	registry plugin.Registry
}

// New creates a Router, builds a reverse proxy for each RouteConfig, and
// wraps each proxy with its configured plugin chain.
func New(routes []RouteConfig, registry plugin.Registry) (*Router, error) {
	r := &Router{
		mux:      http.NewServeMux(),
		registry: registry,
	}
	for _, route := range routes {
		if err := r.addRoute(route); err != nil {
			return nil, fmt.Errorf("route %s: %w", route.Prefix, err)
		}
	}
	return r, nil
}

// ServeHTTP dispatches an incoming request through the matched route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) addRoute(route RouteConfig) error {
	upstream, err := url.Parse(route.Upstream)
	if err != nil {
		return fmt.Errorf("invalid upstream URL %q: %w", route.Upstream, err)
	}

	proxy := buildReverseProxy(upstream, route)
	handler := r.buildPluginChain(proxy, route.Plugins)

	pattern := route.Prefix
	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}

	r.mux.Handle(pattern, handler)
	return nil
}

func buildReverseProxy(upstream *url.URL, route RouteConfig) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstream)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if route.StripPrefix {
			// route.Prefix always ends in "/", so TrimPrefix's result never
			// starts with "/" -- re-add it, or the outgoing request line
			// becomes a malformed relative path ("auth/register" instead of
			// "/auth/register"), which net/http's server rejects outright.
			stripped := strings.TrimPrefix(req.URL.Path, route.Prefix)
			if !strings.HasPrefix(stripped, "/") {
				stripped = "/" + stripped
			}
			req.URL.Path = stripped
		}
		req.Host = upstream.Host
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w,
			fmt.Sprintf(`{"error":"upstream unavailable","detail":"%s"}`, err.Error()),
			http.StatusBadGateway,
		)
	}

	return proxy
}

func (r *Router) buildPluginChain(handler http.Handler, pluginNames []string) http.Handler {
	var plugins []plugin.Plugin
	for _, name := range pluginNames {
		p := r.registry.Get(name)
		if p == nil {
			log.Printf("WARNING: unknown plugin %q", name)
			continue
		}
		plugins = append(plugins, p)
	}
	return plugin.NewChain(handler, plugins...)
}
