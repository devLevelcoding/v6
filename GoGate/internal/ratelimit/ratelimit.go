// Package ratelimit is a keyed token-bucket limiter. GoGate keys it by the
// authenticated subject when there is one, otherwise by client IP, and applies
// the per-route Rate. Buckets refill lazily (no per-bucket goroutine) and idle
// ones are swept so the map does not grow without bound.
package ratelimit

import (
	"sync"
	"time"

	"github.com/levelcodingdev/gogate/internal/route"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter holds one bucket per key.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
	idle    time.Duration
	lastGC  time.Time
}

// New returns a limiter that forgets a key after it has been idle for `idleTTL`
// (default 10m).
func New(idleTTL time.Duration) *Limiter {
	if idleTTL <= 0 {
		idleTTL = 10 * time.Minute
	}
	return &Limiter{buckets: map[string]*bucket{}, now: time.Now, idle: idleTTL}
}

// Allow charges one token against key under rate r. A zero Rate always allows.
// When denied it returns the wait until the next token is available.
func (l *Limiter) Allow(key string, r route.Rate) (ok bool, retryAfter time.Duration) {
	if r.Zero() {
		return true, 0
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.gc(now)

	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(r.Burst), last: now}
		l.buckets[key] = b
	}
	// Lazy refill: add PerSecond tokens per elapsed second, capped at Burst.
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = min(float64(r.Burst), b.tokens+elapsed*r.PerSecond)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	need := (1 - b.tokens) / r.PerSecond
	return false, time.Duration(need * float64(time.Second))
}

func (l *Limiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < l.idle {
		return
	}
	l.lastGC = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.idle {
			delete(l.buckets, k)
		}
	}
}

// Len is the number of tracked keys (for stats/tests).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
