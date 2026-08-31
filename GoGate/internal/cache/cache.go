// Package cache is GoGate's read-through response cache with request
// coalescing. A cacheable request first looks in a TTL store; on a miss, one
// goroutine per key does the upstream call while the others wait on its result
// (golang.org/x/sync/singleflight, so a cache-stampede is one upstream request,
// not N). In-memory only; a shared/Redis cache is a later phase (see
// ../../future.md §3).
package cache

import (
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/levelcodingdev/gogate/internal/ttlcache"
)

// Response is a captured upstream reply, safe to replay from the cache.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Cache is a bounded TTL store (the generic ttlcache.Cache — CoverGo P1) plus a
// singleflight group for coalescing.
type Cache struct {
	store *ttlcache.Cache[string, *Response]
	group singleflight.Group
	now   func() time.Time

	hits     atomic.Uint64
	misses   atomic.Uint64 // real upstream fills (one per coalesced group)
	missPath atomic.Uint64 // requests that reached the miss path
}

// New returns a cache holding at most maxEntries (default 4096). When full, the
// next write drops the whole store — a crude but allocation-cheap reset that is
// fine for an edge cache; an LRU is a later refinement.
func New(maxEntries int) *Cache { return newClock(maxEntries, time.Now) }

// newClock is New with an injectable clock (tests).
func newClock(maxEntries int, clk func() time.Time) *Cache {
	return &Cache{store: ttlcache.New[string, *Response](maxEntries, clk), now: clk}
}

// Do returns the response for key: a live cache entry, a coalesced in-flight
// result, or the outcome of fill(). A nil error with a cacheable 2xx GET/HEAD
// response is stored for ttl. fromCache is true only for a store hit.
func (c *Cache) Do(key string, ttl time.Duration, fill func() (*Response, error)) (resp *Response, fromCache bool, err error) {
	if r, ok := c.store.Get(key); ok {
		c.hits.Add(1)
		return r, true, nil
	}

	c.missPath.Add(1)
	// The fill + the decision to cache both run inside the singleflight closure,
	// so they happen exactly once per stampede. A panic in fill() is recovered
	// by singleflight and re-raised in every waiter — it can't wedge the key.
	v, err, _ := c.group.Do(key, func() (any, error) {
		c.misses.Add(1)
		r, e := fill()
		if e != nil {
			return nil, e
		}
		if ttl > 0 && cacheable(r) {
			c.store.Set(key, r, ttl)
		}
		return r, nil
	})
	if err != nil {
		return nil, false, err
	}
	return v.(*Response), false, nil
}

// Stats is a point-in-time snapshot.
type Stats struct {
	Entries, Hits, Misses, Coalesced int
}

func (c *Cache) Stats() Stats {
	missPath := c.missPath.Load()
	misses := c.misses.Load()
	return Stats{
		Entries:   c.store.Len(),
		Hits:      int(c.hits.Load()),
		Misses:    int(misses),
		Coalesced: int(missPath - misses),
	}
}

func cacheable(r *Response) bool {
	if r == nil || r.Status < 200 || r.Status >= 300 {
		return false
	}
	for _, tok := range strings.Split(strings.ToLower(r.Header.Get("Cache-Control")), ",") {
		switch strings.TrimSpace(tok) {
		case "no-store", "private":
			return false
		}
	}
	return true
}
