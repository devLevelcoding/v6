// Package events is a per-job progress fan-out. A worker Publishes progress as a
// job runs; HTTP clients Subscribe for the Server-Sent-Events stream. Purely
// in-memory and best-effort: a subscriber that can't keep up misses ticks but
// never blocks the worker.
package events

import "sync"

// Event is one progress update for a job.
type Event struct {
	JobID    string  `json:"job_id"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Message  string  `json:"message,omitempty"`
}

// Broker routes Events to the subscribers of each job id.
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[chan Event]struct{})}
}

// Subscribe returns a channel of Events for one job and a cancel func that
// closes it. The channel is buffered; Publish drops rather than blocks when it
// is full.
func (b *Broker) Subscribe(jobID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	if b.subs[jobID] == nil {
		b.subs[jobID] = make(map[chan Event]struct{})
	}
	b.subs[jobID][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if m := b.subs[jobID]; m != nil {
				delete(m, ch)
				if len(m) == 0 {
					delete(b.subs, jobID)
				}
			}
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish delivers e to every current subscriber of e.JobID, skipping any whose
// buffer is full.
func (b *Broker) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[e.JobID] {
		select {
		case ch <- e:
		default:
		}
	}
}

// SubscriberCount is the number of live subscribers for a job (for tests/metrics).
func (b *Broker) SubscriberCount(jobID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[jobID])
}
