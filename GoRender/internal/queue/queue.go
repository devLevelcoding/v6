// Package queue is the job backlog between the API and the worker pool. Mem is
// an in-process channel; a later phase swaps it for Redis or NATS without the
// worker or server noticing — see ../../future.md §3.
package queue

import "context"

// Mem is an in-memory FIFO of job ids with a bounded buffer.
type Mem struct {
	ch chan string
}

// NewMem returns a queue that buffers up to `capacity` waiting jobs before Push
// blocks.
func NewMem(capacity int) *Mem {
	if capacity < 1 {
		capacity = 1
	}
	return &Mem{ch: make(chan string, capacity)}
}

// Push enqueues a job id. It blocks if the buffer is full or ctx is live;
// returns ctx.Err() if ctx is cancelled first.
func (q *Mem) Push(ctx context.Context, id string) error {
	select {
	case q.ch <- id:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Claim blocks until a job id is available or ctx is done. The bool is false
// only when ctx was cancelled.
func (q *Mem) Claim(ctx context.Context) (string, bool) {
	select {
	case id := <-q.ch:
		return id, true
	case <-ctx.Done():
		return "", false
	}
}

// Len is the number of jobs waiting to be claimed.
func (q *Mem) Len() int { return len(q.ch) }
