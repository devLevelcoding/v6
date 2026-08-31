package ratelimit

import (
	"testing"
	"time"

	"github.com/levelcodingdev/gogate/internal/route"
)

func TestZeroRateAlwaysAllows(t *testing.T) {
	l := New(0)
	for i := 0; i < 100; i++ {
		if ok, _ := l.Allow("k", route.Rate{}); !ok {
			t.Fatal("zero Rate must always allow")
		}
	}
}

func TestBurstThenDenyThenRefill(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(0)
	l.now = func() time.Time { return now }

	r := route.Rate{PerSecond: 2, Burst: 3}
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k", r); !ok {
			t.Fatalf("burst token %d denied", i)
		}
	}
	ok, retry := l.Allow("k", r)
	if ok {
		t.Fatal("4th request should be denied — burst spent")
	}
	if retry <= 0 || retry > time.Second {
		t.Fatalf("Retry-After = %v, want ~0.5s", retry)
	}

	now = now.Add(time.Second) // +2 tokens
	if ok, _ := l.Allow("k", r); !ok {
		t.Fatal("token should have refilled after 1s")
	}
	if ok, _ := l.Allow("k", r); !ok {
		t.Fatal("second refilled token")
	}
	if ok, _ := l.Allow("k", r); ok {
		t.Fatal("only 2 tokens refilled")
	}
}

func TestPerKeyIsolation(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(0)
	l.now = func() time.Time { return now }
	r := route.Rate{PerSecond: 1, Burst: 1}

	if ok, _ := l.Allow("a", r); !ok {
		t.Fatal("a first")
	}
	if ok, _ := l.Allow("a", r); ok {
		t.Fatal("a second denied")
	}
	if ok, _ := l.Allow("b", r); !ok {
		t.Fatal("b has its own bucket")
	}
	if l.Len() != 2 {
		t.Fatalf("Len = %d, want 2", l.Len())
	}
}

func TestIdleGC(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(time.Minute)
	l.now = func() time.Time { return now }
	r := route.Rate{PerSecond: 1, Burst: 1}

	l.Allow("stale", r)
	now = now.Add(2 * time.Minute)
	l.Allow("fresh", r) // triggers gc, which drops "stale"
	if l.Len() != 1 {
		t.Fatalf("Len after GC = %d, want 1", l.Len())
	}
}
