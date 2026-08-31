// Package route is GoGate's routing table: which incoming (host, path) goes to
// which upstream, and the policy — auth, rate limit, cache — that applies on the
// way. The in-memory Store here stands in for the config file / database a
// later phase adds (see ../../future.md §3).
package route

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrInvalid is returned when a route fails validation.
	ErrInvalid = errors.New("route: invalid")
	// ErrNotFound is returned when a route id is unknown.
	ErrNotFound = errors.New("route: not found")
)

// Rate is a token-bucket rate limit. A zero Rate means "no limit".
type Rate struct {
	PerSecond float64 `json:"per_second"`
	Burst     int     `json:"burst"`
}

// Zero reports whether the rate is unset (no limiting).
func (r Rate) Zero() bool { return r.PerSecond <= 0 && r.Burst <= 0 }

// Policy is what GoGate enforces for a route before it reaches the upstream.
type Policy struct {
	RequireAuth bool          `json:"require_auth"`
	RateLimit   Rate          `json:"rate_limit"`
	CacheTTL        time.Duration `json:"cache_ttl"`         // >0 → cache GET/HEAD responses this long
	MaxInFlight     int           `json:"max_in_flight"`     // >0 → cap concurrent requests to the upstream; excess gets 503
	UpstreamTimeout time.Duration `json:"upstream_timeout"`  // >0 → per-route response-header timeout on the upstream
}

// Target is where a matched request goes. Exactly one field is set: Upstream
// for an HTTP reverse proxy, Subject for the HTTP↔queue bridge.
type Target struct {
	Upstream string `json:"upstream,omitempty"`
	Subject  string `json:"subject,omitempty"`
}

// Route is one entry in the table.
type Route struct {
	ID          string `json:"id"`
	Host        string `json:"host,omitempty"` // "" matches any host
	PathPrefix  string `json:"path_prefix"`    // must start with "/"
	StripPrefix bool   `json:"strip_prefix"`   // drop PathPrefix before forwarding
	Target      Target `json:"target"`
	Policy      Policy `json:"policy"`

	seq int64 // insertion order — longest-prefix ties break by most-recent
}

// IsBridge reports whether the route forwards over the queue bridge rather than
// an HTTP upstream.
func (r Route) IsBridge() bool { return r.Target.Subject != "" }

// Validate reports the first problem with r (id, seq and defaults aside).
func (r Route) Validate() error {
	if !strings.HasPrefix(r.PathPrefix, "/") {
		return fmt.Errorf("%w: path_prefix must start with \"/\"", ErrInvalid)
	}
	up, sub := strings.TrimSpace(r.Target.Upstream), strings.TrimSpace(r.Target.Subject)
	switch {
	case up == "" && sub == "":
		return fmt.Errorf("%w: target needs an upstream or a subject", ErrInvalid)
	case up != "" && sub != "":
		return fmt.Errorf("%w: target has both an upstream and a subject", ErrInvalid)
	case up != "":
		u, err := url.Parse(up)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%w: upstream %q is not an absolute URL", ErrInvalid, up)
		}
	}
	if rl := r.Policy.RateLimit; !rl.Zero() {
		if rl.PerSecond <= 0 || rl.Burst <= 0 {
			return fmt.Errorf("%w: rate_limit needs per_second > 0 and burst > 0", ErrInvalid)
		}
	}
	if r.Policy.CacheTTL < 0 {
		return fmt.Errorf("%w: cache_ttl must not be negative", ErrInvalid)
	}
	if r.Policy.MaxInFlight < 0 {
		return fmt.Errorf("%w: max_in_flight must not be negative", ErrInvalid)
	}
	if r.Policy.UpstreamTimeout < 0 {
		return fmt.Errorf("%w: upstream_timeout must not be negative", ErrInvalid)
	}
	return nil
}

// Cacheable reports whether responses for method m on this route may be cached.
func (r Route) Cacheable(method string) bool {
	return r.Policy.CacheTTL > 0 && (method == "GET" || method == "HEAD")
}
