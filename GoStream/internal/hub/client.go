package hub

import (
	"sync"
	"time"
)

// Meta is the descriptive info attached to a connection at subscribe time.
type Meta struct {
	Subject    string // authenticated identity, "" if anonymous
	RemoteAddr string
}

// Client is one subscriber. Its send channel is bounded; a Publish that would
// block on it drops the message and bumps Dropped instead. Too many drops and
// the hub kills the client (slow-consumer eviction).
type Client struct {
	ID   string
	Meta Meta

	send       chan []byte
	dead       chan struct{}
	closeOnce  sync.Once
	killReason string

	mu         sync.Mutex
	topics     map[string]struct{}
	dropped    int
	maxDropped int

	connectedAt time.Time
}

func newClient(id string, meta Meta, buffer, maxDropped int) *Client {
	if buffer < 1 {
		buffer = 1
	}
	if maxDropped < 0 {
		maxDropped = 0
	}
	return &Client{
		ID:          id,
		Meta:        meta,
		send:        make(chan []byte, buffer),
		dead:        make(chan struct{}),
		topics:      map[string]struct{}{},
		maxDropped:  maxDropped,
		connectedAt: time.Now(),
	}
}

// Out is the channel the connection's writer goroutine drains.
func (c *Client) Out() <-chan []byte { return c.send }

// Done is closed when the client is killed; the writer goroutine should return.
func (c *Client) Done() <-chan struct{} { return c.dead }

// KillReason is set once Done is closed.
func (c *Client) KillReason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.killReason
}

// Kill marks the client dead exactly once.
func (c *Client) Kill(reason string) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.killReason = reason
		c.mu.Unlock()
		close(c.dead)
	})
}

// enqueue is a non-blocking send. It returns false (and counts a drop) when the
// buffer is full; the caller decides whether the drop count warrants a Kill.
func (c *Client) enqueue(msg []byte) bool {
	select {
	case <-c.dead:
		return false
	case c.send <- msg:
		return true
	default:
		c.mu.Lock()
		c.dropped++
		over := c.dropped > c.maxDropped
		c.mu.Unlock()
		if over {
			c.Kill("slow consumer: send buffer overrun")
		}
		return false
	}
}

// TopicList is a snapshot of the client's subscriptions.
func (c *Client) TopicList() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.topics))
	for t := range c.topics {
		out = append(out, t)
	}
	return out
}
