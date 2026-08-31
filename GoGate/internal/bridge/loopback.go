package bridge

import (
	"context"
	"sync"
)

// Loopback is an in-process Transport: subjects map to Go funcs. It is what a
// single-binary GoGate deployment (and every bridge test) uses in place of a
// message broker.
type Loopback struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

// HandlerFunc serves one subject.
type HandlerFunc func(ctx context.Context, m Message) Reply

// NewLoopback returns an empty transport.
func NewLoopback() *Loopback { return &Loopback{handlers: map[string]HandlerFunc{}} }

// Handle registers fn for subject, replacing any previous handler.
func (l *Loopback) Handle(subject string, fn HandlerFunc) {
	l.mu.Lock()
	l.handlers[subject] = fn
	l.mu.Unlock()
}

// Request dispatches m to the subject's handler.
func (l *Loopback) Request(ctx context.Context, subject string, m Message) (Reply, error) {
	l.mu.RLock()
	fn, ok := l.handlers[subject]
	l.mu.RUnlock()
	if !ok {
		return Reply{}, ErrNoHandler
	}

	done := make(chan Reply, 1)
	go func() { done <- fn(ctx, m) }()
	select {
	case r := <-done:
		return r, nil
	case <-ctx.Done():
		return Reply{}, ctx.Err()
	}
}

// Subjects lists the registered subjects (for stats/tests).
func (l *Loopback) Subjects() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, 0, len(l.handlers))
	for s := range l.handlers {
		out = append(out, s)
	}
	return out
}
