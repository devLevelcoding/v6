// Package hub is GoStream's fan-out core: a topic→subscribers index and a
// non-blocking Publish. It knows nothing about WebSockets — it deals in client
// ids, topic strings and byte payloads, so the transport (internal/ws) and the
// publish ingress (internal/ingest) are swappable. Slow subscribers are
// dropped, not waited on.
package hub

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levelcodingdev/gostream/internal/uid"
)

// targetPool reuses the []*Client scratch slice Publish fills under the read
// lock (CoverGo U7 — it was the whole per-publish allocation, scaling with the
// subscriber count; see PROFILING.md).
var targetPool = sync.Pool{New: func() any { s := make([]*Client, 0, 64); return &s }}

// Config tunes new clients.
type Config struct {
	SendBuffer int // per-client queued-message cap before drops (default 64)
	MaxDropped int // drops tolerated before the client is evicted (default 32)
}

// Hub holds every client and the topic index.
type Hub struct {
	cfg Config

	mu      sync.RWMutex
	clients map[string]*Client
	topics  map[string]map[string]struct{} // topic -> set of client ids

	published atomic.Uint64
	delivered atomic.Uint64
	dropped   atomic.Uint64
}

// New returns an empty hub.
func New(cfg Config) *Hub {
	if cfg.SendBuffer <= 0 {
		cfg.SendBuffer = 64
	}
	if cfg.MaxDropped < 0 {
		cfg.MaxDropped = 0
	}
	return &Hub{
		cfg:     cfg,
		clients: map[string]*Client{},
		topics:  map[string]map[string]struct{}{},
	}
}

// Add registers a new client and returns it. The caller runs the connection's
// read/write loops and calls Remove when they end.
func (h *Hub) Add(meta Meta) *Client {
	c := newClient(uid.New()[:16], meta, h.cfg.SendBuffer, h.cfg.MaxDropped)
	h.mu.Lock()
	h.clients[c.ID] = c
	h.mu.Unlock()
	return c
}

// Remove drops the client from every topic and the registry, and kills it.
func (h *Hub) Remove(c *Client) {
	h.mu.Lock()
	delete(h.clients, c.ID)
	for _, ids := range h.topics {
		delete(ids, c.ID)
	}
	for t, ids := range h.topics {
		if len(ids) == 0 {
			delete(h.topics, t)
		}
	}
	h.mu.Unlock()
	c.Kill("removed")
}

// Subscribe adds c to topic.
func (h *Hub) Subscribe(c *Client, topic string) {
	h.mu.Lock()
	if h.topics[topic] == nil {
		h.topics[topic] = map[string]struct{}{}
	}
	h.topics[topic][c.ID] = struct{}{}
	h.mu.Unlock()

	c.mu.Lock()
	c.topics[topic] = struct{}{}
	c.mu.Unlock()
}

// Unsubscribe removes c from topic.
func (h *Hub) Unsubscribe(c *Client, topic string) {
	h.mu.Lock()
	if ids := h.topics[topic]; ids != nil {
		delete(ids, c.ID)
		if len(ids) == 0 {
			delete(h.topics, topic)
		}
	}
	h.mu.Unlock()

	c.mu.Lock()
	delete(c.topics, topic)
	c.mu.Unlock()
}

// Result is what one Publish did.
type Result struct {
	Delivered int `json:"delivered"`
	Dropped   int `json:"dropped"`
}

// Publish fans msg out to every current subscriber of topic without blocking on
// any of them.
func (h *Hub) Publish(topic string, msg []byte) Result {
	tp := targetPool.Get().(*[]*Client)
	targets := (*tp)[:0]

	// One pass: resolve ids to *Client directly, no intermediate []string.
	h.mu.RLock()
	for id := range h.topics[topic] {
		if c := h.clients[id]; c != nil {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	var res Result
	for _, c := range targets {
		if c.enqueue(msg) {
			res.Delivered++
		} else {
			res.Dropped++
		}
	}

	// Return the (possibly grown) slice to the pool; clear it so it doesn't
	// pin dropped clients.
	for i := range targets {
		targets[i] = nil
	}
	*tp = targets[:0]
	targetPool.Put(tp)

	h.published.Add(1)
	h.delivered.Add(uint64(res.Delivered))
	h.dropped.Add(uint64(res.Dropped))
	return res
}

// ClientInfo is one row of presence.
type ClientInfo struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject,omitempty"`
	RemoteAddr  string    `json:"remote_addr,omitempty"`
	Topics      []string  `json:"topics"`
	ConnectedAt time.Time `json:"connected_at"`
}

// Presence is a snapshot of every connected client.
func (h *Hub) Presence() []ClientInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ClientInfo, 0, len(h.clients))
	for _, c := range h.clients {
		out = append(out, ClientInfo{
			ID: c.ID, Subject: c.Meta.Subject, RemoteAddr: c.Meta.RemoteAddr,
			Topics: c.TopicList(), ConnectedAt: c.connectedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConnectedAt.Before(out[j].ConnectedAt) })
	return out
}

// Stats is a point-in-time snapshot of hub counters.
type Stats struct {
	Clients   int    `json:"clients"`
	Topics    int    `json:"topics"`
	Published uint64 `json:"published"`
	Delivered uint64 `json:"delivered"`
	Dropped   uint64 `json:"dropped"`
}

func (h *Hub) Stats() Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return Stats{
		Clients: len(h.clients), Topics: len(h.topics),
		Published: h.published.Load(), Delivered: h.delivered.Load(), Dropped: h.dropped.Load(),
	}
}

// TopicSize is the subscriber count for one topic.
func (h *Hub) TopicSize(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic])
}
