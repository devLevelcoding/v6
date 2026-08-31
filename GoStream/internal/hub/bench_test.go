package hub

import (
	"strconv"
	"testing"
)

// CoverGo U1 — the per-subscriber cost of a topic broadcast and the overhead of
// dropping to a slow consumer. featureGo.md claims 15k→300k connections/instance
// on this path; `benchstat` before/after any hub change.

func benchHub(b *testing.B, subs int, drain bool) *Hub {
	b.Helper()
	h := New(Config{SendBuffer: 4, MaxDropped: 1 << 30})
	for i := 0; i < subs; i++ {
		c := h.Add(Meta{})
		h.Subscribe(c, "firehose")
		if drain {
			go func(c *Client) {
				for {
					select {
					case <-c.Out():
					case <-c.Done():
						return
					}
				}
			}(c)
		}
	}
	return h
}

// BenchmarkPublishFanout sweeps subscriber count; divide ns/op by N for the
// per-subscriber cost.
func BenchmarkPublishFanout(b *testing.B) {
	for _, n := range []int{100, 1000, 10000, 50000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			h := benchHub(b, n, true)
			msg := []byte("payload")
			b.ReportAllocs()
			for b.Loop() {
				h.Publish("firehose", msg)
			}
		})
	}
}

// BenchmarkPublishAllSlow measures the broadcast cost when every subscriber's
// buffer is full — the message is dropped per consumer, never queued.
func BenchmarkPublishAllSlow(b *testing.B) {
	h := benchHub(b, 1000, false) // no drain → buffers fill and stay full
	msg := []byte("payload")
	// fill every send buffer
	for i := 0; i < 8; i++ {
		h.Publish("firehose", msg)
	}
	b.ReportAllocs()
	for b.Loop() {
		h.Publish("firehose", msg)
	}
}

// BenchmarkSubscribeChurn measures add+subscribe+remove — the connect/disconnect
// path at 300k conns must not lock the whole hub.
func BenchmarkSubscribeChurn(b *testing.B) {
	h := New(Config{SendBuffer: 4})
	b.ReportAllocs()
	for b.Loop() {
		c := h.Add(Meta{})
		h.Subscribe(c, "room")
		h.Unsubscribe(c, "room")
		h.Remove(c)
	}
}
