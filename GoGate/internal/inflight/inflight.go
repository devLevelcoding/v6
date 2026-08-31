// Package inflight caps concurrent in-flight requests per key (CoverGo U18),
// so a weak upstream can't be overwhelmed by the gateway's own concurrency.
// Each key gets a golang.org/x/sync/semaphore.Weighted; a caller that can't get
// a slot within a short grace period is turned away (the gateway returns 503).
package inflight

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// Limiter holds one semaphore per key.
type Limiter struct {
	mu    sync.Mutex
	sems  map[string]*semaphore.Weighted
	grace time.Duration
}

// New returns a limiter whose Acquire waits up to grace (default 100ms) for a
// slot before giving up. A zero or negative grace means "fail fast".
func New(grace time.Duration) *Limiter {
	if grace == 0 {
		grace = 100 * time.Millisecond
	}
	return &Limiter{sems: map[string]*semaphore.Weighted{}, grace: grace}
}

func (l *Limiter) sem(key string, max int) *semaphore.Weighted {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.sems[key]
	if s == nil {
		s = semaphore.NewWeighted(int64(max))
		l.sems[key] = s
	}
	return s
}

// Acquire takes one slot for key, bounded to max concurrent. It returns a
// release func and true on success; nil and false if max <= 0 disables the
// limit (release is a no-op) or if no slot came free within the grace period.
func (l *Limiter) Acquire(ctx context.Context, key string, max int) (release func(), ok bool) {
	if max <= 0 {
		return func() {}, true
	}
	s := l.sem(key, max)
	actx, cancel := context.WithTimeout(ctx, l.grace)
	defer cancel()
	if err := s.Acquire(actx, 1); err != nil {
		return nil, false
	}
	return func() { s.Release(1) }, true
}
