package server

import (
	"net"
	"net/http"
	"strings"

	"github.com/levelcodingdev/gogate/internal/auth"
	"github.com/levelcodingdev/gogate/internal/bridge"
	"github.com/levelcodingdev/gogate/internal/cache"
	"github.com/levelcodingdev/gogate/internal/route"
)

// forward strips the prefix, injects the verified identity, and runs the
// (optionally cached) upstream call.
func (s *server) forward(w http.ResponseWriter, r *http.Request, rt route.Route, claims auth.Claims, authed bool) {
	if rt.StripPrefix && rt.PathPrefix != "/" {
		trimmed := strings.TrimPrefix(r.URL.Path, strings.TrimRight(rt.PathPrefix, "/"))
		if trimmed == "" {
			trimmed = "/"
		}
		r.URL.Path = trimmed
	}
	auth.Inject(r, claims, authed)

	if s.cfg.Cache == nil || !rt.Cacheable(r.Method) {
		s.dispatch(w, r, rt)
		return
	}

	key := cacheKey(r, claims, authed)
	resp, fromCache, _ := s.cfg.Cache.Do(key, rt.Policy.CacheTTL, func() (*cache.Response, error) {
		cw := getCapture()
		defer putCapture(cw)
		s.dispatch(cw, r, rt)
		return cw.response(), nil
	})
	state := "MISS"
	if fromCache {
		state = "HIT"
	}
	replay(w, resp, state)
}

// dispatch is the last hop: HTTP upstream or the queue bridge.
func (s *server) dispatch(w http.ResponseWriter, r *http.Request, rt route.Route) {
	if rt.IsBridge() {
		if s.cfg.Bridge == nil {
			writeErr(w, http.StatusBadGateway, "bridge not configured")
			return
		}
		bridge.Handler{Transport: s.cfg.Bridge}.ServeSubject(w, r, rt.Target.Subject)
		return
	}
	rp, err := s.cfg.Proxy.ForRoute(rt.Target.Upstream, rt.Policy.UpstreamTimeout)
	if err != nil {
		s.cfg.Log.Error("gogate: bad upstream", "route", rt.ID, "err", err)
		writeErr(w, http.StatusBadGateway, "bad upstream")
		return
	}
	rp.ServeHTTP(w, r)
}

// cacheKey varies by method, host, path, query and — when the request is
// authenticated — the subject, so one user's cached response never leaks to
// another.
func cacheKey(r *http.Request, c auth.Claims, authed bool) string {
	var b strings.Builder
	b.WriteString(r.Method)
	b.WriteByte(' ')
	b.WriteString(strings.ToLower(r.Host))
	b.WriteString(r.URL.Path)
	if r.URL.RawQuery != "" {
		b.WriteByte('?')
		b.WriteString(r.URL.RawQuery)
	}
	if authed && c.Subject != "" {
		b.WriteString("\x00sub:")
		b.WriteString(c.Subject)
	}
	return b.String()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
