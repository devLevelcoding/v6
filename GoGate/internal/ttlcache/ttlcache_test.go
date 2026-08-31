package ttlcache

import (
	"strconv"
	"testing"
	"time"
)

func TestGetSetExpiry(t *testing.T) {
	now := time.Unix(0, 0)
	c := New[string, int](0, func() time.Time { return now })

	c.Set("k", 42, time.Minute)
	if v, ok := c.Get("k"); !ok || v != 42 {
		t.Fatalf("Get after Set = %d, %v", v, ok)
	}

	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("k"); ok {
		t.Fatal("entry should have expired")
	}

	// zero/negative ttl is a no-op
	c.Set("z", 1, 0)
	if _, ok := c.Get("z"); ok {
		t.Fatal("ttl<=0 must not store")
	}
}

func TestSizeCapClears(t *testing.T) {
	c := New[string, int](2, nil)
	for i := 0; i < 5; i++ {
		c.Set(strconv.Itoa(i), i, time.Hour)
	}
	if c.Len() > 2 {
		t.Fatalf("Len = %d, cap 2", c.Len())
	}
}

func TestTypedValues(t *testing.T) {
	type row struct{ name string }
	c := New[int, *row](0, nil)
	c.Set(7, &row{name: "seven"}, time.Hour)
	if v, ok := c.Get(7); !ok || v.name != "seven" {
		t.Fatalf("typed value round-trip failed: %+v %v", v, ok)
	}
	if _, ok := c.Get(8); ok {
		t.Fatal("miss should return zero value + false")
	}
}
