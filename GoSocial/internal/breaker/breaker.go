// Package breaker implements a generic circuit breaker for wrapping calls
// that can fail (e.g. RPCs to another service).
package breaker

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// State represents the current state of a circuit breaker.
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// Config holds the tuning parameters for a CircuitBreaker.
type Config struct {
	// MaxFailures is the number of consecutive failures that trip the breaker.
	MaxFailures int

	// Timeout is how long the breaker stays Open before transitioning to
	// HalfOpen to allow a probe request through.
	Timeout time.Duration

	// SuccessThreshold is the number of consecutive successes in HalfOpen
	// state needed to transition back to Closed.
	SuccessThreshold int

	// OnStateChange is an optional callback invoked whenever the state changes.
	OnStateChange func(from, to State)
}

// DefaultConfig returns a sensible default Config.
func DefaultConfig() Config {
	return Config{
		MaxFailures:      5,
		Timeout:          10 * time.Second,
		SuccessThreshold: 2,
	}
}

// Metrics is a snapshot of circuit breaker statistics.
type Metrics struct {
	State           State
	Failures        int
	Successes       int
	TotalRequests   int64
	LastStateChange time.Time
}

// CircuitBreaker wraps any operation returning (T, error) and is safe for
// concurrent use.
type CircuitBreaker[T any] struct {
	config Config

	mu              sync.Mutex
	state           State
	failures        int
	successes       int
	lastFailure     time.Time
	lastStateChange time.Time
	totalRequests   int64
}

// New creates a CircuitBreaker with the given Config, filling in defaults
// for any zero-valued fields.
func New[T any](cfg Config) *CircuitBreaker[T] {
	def := DefaultConfig()
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = def.MaxFailures
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = def.SuccessThreshold
	}
	return &CircuitBreaker[T]{
		config:          cfg,
		state:           StateClosed,
		lastStateChange: time.Now(),
	}
}

// Execute runs fn through the circuit breaker:
//   - Open: if the timeout has elapsed, transition to HalfOpen and allow the
//     call through as a probe; otherwise fail fast with ErrCircuitOpen.
//   - HalfOpen: allow the call; success counts toward SuccessThreshold and
//     eventually closes the breaker, any failure re-opens it immediately.
//   - Closed: allow the call; failures accumulate until MaxFailures trips
//     the breaker open.
func (cb *CircuitBreaker[T]) Execute(ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	cb.mu.Lock()
	cb.totalRequests++

	if cb.state == StateOpen {
		if time.Since(cb.lastFailure) < cb.config.Timeout {
			cb.mu.Unlock()
			var zero T
			return zero, ErrCircuitOpen
		}
		cb.transition(StateHalfOpen)
	}
	cb.mu.Unlock()

	// Run the call itself outside the lock so a slow/blocked fn doesn't stall
	// other goroutines checking the breaker's state.
	result, err := fn(ctx)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
		return result, err
	}
	cb.onSuccess()
	return result, nil
}

// Metrics returns a snapshot of the current breaker statistics.
func (cb *CircuitBreaker[T]) Metrics() Metrics {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return Metrics{
		State:           cb.state,
		Failures:        cb.failures,
		Successes:       cb.successes,
		TotalRequests:   cb.totalRequests,
		LastStateChange: cb.lastStateChange,
	}
}

// Reset forces the breaker back to Closed with all counters zeroed.
func (cb *CircuitBreaker[T]) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	prev := cb.state
	cb.state = StateClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastStateChange = time.Now()

	if cb.config.OnStateChange != nil && prev != StateClosed {
		cb.config.OnStateChange(prev, StateClosed)
	}
}

func (cb *CircuitBreaker[T]) onFailure() {
	cb.failures++
	cb.successes = 0
	cb.lastFailure = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.config.MaxFailures {
			cb.transition(StateOpen)
		}
	case StateHalfOpen:
		cb.transition(StateOpen)
	}
}

func (cb *CircuitBreaker[T]) onSuccess() {
	cb.failures = 0

	switch cb.state {
	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transition(StateClosed)
		}
	case StateClosed:
		cb.successes++
	}
}

// transition moves the breaker to a new state and notifies OnStateChange.
// Must be called with cb.mu held.
func (cb *CircuitBreaker[T]) transition(to State) {
	from := cb.state
	if from == to {
		return
	}

	cb.state = to
	cb.lastStateChange = time.Now()
	cb.successes = 0
	if to == StateOpen {
		cb.lastFailure = time.Now()
	}

	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(from, to)
	}
}
