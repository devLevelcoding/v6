// Package ttlcache is a small generic TTL map (CoverGo P1): the "map + mutex +
// per-entry expiry + bounded size" pattern GoGate was hand-rolling in a couple
// of places. When full it drops the whole store — a crude, allocation-cheap
// reset that suits an edge cache; an LRU is a later refinement.
package ttlcache

import (
	"sync"
	"time"
)

type entry[V any] struct {
	val     V
	expires time.Time
}

// Cache is a bounded key→value store with per-entry TTLs. The zero value is not
// usable; call New.
type Cache[K comparable, V any] struct {
	mu    sync.Mutex
	items map[K]entry[V]
	max   int
	now   func() time.Time
}

// New returns a cache holding at most max entries (<=0 → 4096). nowFn defaults
// to time.Now; tests pass a fake clock.
func New[K comparable, V any](max int, nowFn func() time.Time) *Cache[K, V] {
	if max <= 0 {
		max = 4096
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Cache[K, V]{items: make(map[K]entry[V]), max: max, now: nowFn}
}

// Get returns the live value for k. The bool is false on a miss or an expired
// entry (which is left for the next Set to evict — cheap).
func (c *Cache[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[k]
	if !ok || !c.now().Before(e.expires) {
		var zero V
		return zero, false
	}
	return e.val, true
}

// Set stores v under k for ttl. A ttl <= 0 is a no-op. When the store is at
// capacity it is cleared first.
func (c *Cache[K, V]) Set(k K, v V, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, present := c.items[k]; !present && len(c.items) >= c.max {
		c.items = make(map[K]entry[V])
	}
	c.items[k] = entry[V]{val: v, expires: c.now().Add(ttl)}
}

// Len is the current entry count (including not-yet-evicted expired ones).
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
