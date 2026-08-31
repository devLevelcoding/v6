package cache

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func resp(status int, body string, h http.Header) *Response {
	if h == nil {
		h = http.Header{}
	}
	return &Response{Status: status, Header: h, Body: []byte(body)}
}

func TestMissThenHitThenExpire(t *testing.T) {
	now := time.Unix(0, 0)
	c := newClock(0, func() time.Time { return now })

	var calls int
	fill := func() (*Response, error) { calls++; return resp(200, "v1", nil), nil }

	r, fromCache, _ := c.Do("k", time.Minute, fill)
	if fromCache || string(r.Body) != "v1" || calls != 1 {
		t.Fatalf("first Do: fromCache=%v body=%s calls=%d", fromCache, r.Body, calls)
	}
	r, fromCache, _ = c.Do("k", time.Minute, fill)
	if !fromCache || calls != 1 {
		t.Fatalf("second Do should hit: fromCache=%v calls=%d", fromCache, calls)
	}

	now = now.Add(2 * time.Minute)
	_, fromCache, _ = c.Do("k", time.Minute, fill)
	if fromCache || calls != 2 {
		t.Fatalf("after TTL: fromCache=%v calls=%d", fromCache, calls)
	}
}

func TestCoalesce(t *testing.T) {
	// synctest (CoverGo U24): synctest.Wait replaces the "sleep 20ms and hope
	// all 20 goroutines piled up" race.
	synctest.Test(t, func(t *testing.T) {
		c := New(0)
		var calls int64
		release := make(chan struct{})
		fill := func() (*Response, error) {
			atomic.AddInt64(&calls, 1)
			<-release // hold the first caller here
			return resp(200, "x", nil), nil
		}

		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); c.Do("k", time.Minute, fill) }()
		}
		synctest.Wait() // 1 goroutine in fill blocked on <-release, 19 coalesced
		close(release)
		wg.Wait()

		if n := atomic.LoadInt64(&calls); n != 1 {
			t.Fatalf("20 concurrent misses did %d fills, want 1", n)
		}
		if s := c.Stats(); s.Coalesced < 19 {
			t.Fatalf("Stats.Coalesced = %d, want >= 19", s.Coalesced)
		}
	})
}

func TestNotCached(t *testing.T) {
	c := New(0)
	fill := func(r *Response) func() (*Response, error) {
		return func() (*Response, error) { return r, nil }
	}
	noStore := http.Header{"Cache-Control": {"no-store"}}

	for name, r := range map[string]*Response{
		"500":      resp(500, "err", nil),
		"no-store": resp(200, "ok", noStore),
	} {
		c.Do("k-"+name, time.Minute, fill(r))
		if _, fromCache, _ := c.Do("k-"+name, time.Minute, fill(resp(200, "second", nil))); fromCache {
			t.Fatalf("%s response must not be cached", name)
		}
	}
}

func TestSizeCapResets(t *testing.T) {
	c := New(2)
	for _, k := range []string{"a", "b", "c"} {
		c.Do(k, time.Minute, func() (*Response, error) { return resp(200, k, nil), nil })
	}
	if s := c.Stats(); s.Entries > 2 {
		t.Fatalf("Entries = %d, cap is 2", s.Entries)
	}
}

// TestFillPanicDoesNotWedge: a panic in fill() must surface to callers, not
// deadlock coalesced waiters or poison the key (the reason for singleflight
// over the old hand-rolled WaitGroup — CoverGo U17).
func TestFillPanicDoesNotWedge(t *testing.T) {
	c := New(0)
	panicky := func() (*Response, error) { panic("upstream client blew up") }

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected the fill panic to propagate")
			}
		}()
		_, _, _ = c.Do("k", time.Minute, panicky)
	}()

	// The key must be usable again — no lingering in-flight call.
	done := make(chan struct{})
	go func() {
		_, _, err := c.Do("k", time.Minute, func() (*Response, error) { return resp(200, "recovered", nil), nil })
		if err != nil {
			t.Errorf("second Do errored: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second Do wedged — the panicked key was not released")
	}
}

func TestFillErrorNotCached(t *testing.T) {
	c := New(0)
	_, _, err := c.Do("k", time.Minute, func() (*Response, error) { return nil, http.ErrHandlerTimeout })
	if err == nil {
		t.Fatal("want the fill error propagated")
	}
	calls := 0
	c.Do("k", time.Minute, func() (*Response, error) { calls++; return resp(200, "ok", nil), nil })
	if calls != 1 {
		t.Fatal("a failed fill must not be cached")
	}
}
