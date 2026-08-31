package hub

import (
	"testing"
	"time"
)

func drain(c *Client) (got [][]byte) {
	for {
		select {
		case m := <-c.Out():
			got = append(got, m)
		default:
			return
		}
	}
}

func TestSubscribePublishIsolation(t *testing.T) {
	h := New(Config{SendBuffer: 8})
	a := h.Add(Meta{})
	b := h.Add(Meta{})

	h.Subscribe(a, "chat")
	h.Subscribe(b, "ops")

	res := h.Publish("chat", []byte("hi"))
	if res.Delivered != 1 || res.Dropped != 0 {
		t.Fatalf("publish result = %+v, want 1/0", res)
	}
	if len(drain(a)) != 1 {
		t.Fatal("a should have the chat message")
	}
	if len(drain(b)) != 0 {
		t.Fatal("b is not subscribed to chat")
	}

	// publish to a topic with no subscribers
	if r := h.Publish("nobody", []byte("x")); r.Delivered != 0 {
		t.Fatalf("orphan publish delivered %d", r.Delivered)
	}
}

func TestUnsubscribeAndRemove(t *testing.T) {
	h := New(Config{})
	c := h.Add(Meta{})
	h.Subscribe(c, "t1")
	h.Subscribe(c, "t2")
	if h.TopicSize("t1") != 1 {
		t.Fatal("t1 should have c")
	}
	h.Unsubscribe(c, "t1")
	if h.TopicSize("t1") != 0 {
		t.Fatal("t1 empty after unsubscribe")
	}
	h.Remove(c)
	if h.TopicSize("t2") != 0 || h.Stats().Clients != 0 {
		t.Fatalf("Remove should drop c from every topic: %+v", h.Stats())
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("Remove should kill the client")
	}
}

func TestSlowConsumerDroppedThenEvicted(t *testing.T) {
	h := New(Config{SendBuffer: 2, MaxDropped: 3})
	slow := h.Add(Meta{})
	h.Subscribe(slow, "firehose")

	// Fill the buffer (2), then 3 tolerated drops, the 4th drop evicts.
	var totalDropped int
	for i := 0; i < 10; i++ {
		totalDropped += h.Publish("firehose", []byte("m")).Dropped
	}
	if totalDropped < 4 {
		t.Fatalf("expected drops once the buffer filled, got %d", totalDropped)
	}
	select {
	case <-slow.Done():
	case <-time.After(time.Second):
		t.Fatal("a persistently slow consumer should be evicted")
	}
	if slow.KillReason() == "" {
		t.Fatal("eviction should record a reason")
	}
}

func TestPresenceAndStats(t *testing.T) {
	h := New(Config{})
	c1 := h.Add(Meta{Subject: "u1", RemoteAddr: "1.2.3.4:5"})
	h.Add(Meta{Subject: "u2"})
	h.Subscribe(c1, "room")

	p := h.Presence()
	if len(p) != 2 {
		t.Fatalf("presence has %d, want 2", len(p))
	}
	byID := map[string]ClientInfo{}
	for _, ci := range p {
		byID[ci.ID] = ci
	}
	if got := byID[c1.ID]; got.Subject != "u1" || len(got.Topics) != 1 || got.Topics[0] != "room" {
		t.Fatalf("presence for c1 = %+v", got)
	}

	h.Publish("room", []byte("x"))
	s := h.Stats()
	if s.Clients != 2 || s.Topics != 1 || s.Published != 1 || s.Delivered != 1 {
		t.Fatalf("stats = %+v", s)
	}
}
