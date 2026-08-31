package inflight

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestDisabledWhenMaxZero(t *testing.T) {
	l := New(0)
	rel, ok := l.Acquire(context.Background(), "k", 0)
	if !ok {
		t.Fatal("max<=0 should always admit")
	}
	rel()
}

func TestCapsConcurrency(t *testing.T) {
	l := New(10 * time.Millisecond)
	rel1, ok := l.Acquire(context.Background(), "up", 2)
	if !ok {
		t.Fatal("1st should get a slot")
	}
	rel2, ok := l.Acquire(context.Background(), "up", 2)
	if !ok {
		t.Fatal("2nd should get a slot")
	}
	if _, ok := l.Acquire(context.Background(), "up", 2); ok {
		t.Fatal("3rd should be refused while both slots are held")
	}

	rel1()
	rel3, ok := l.Acquire(context.Background(), "up", 2)
	if !ok {
		t.Fatal("a slot should be free after a release")
	}
	rel2()
	rel3()

	// A different key is independent.
	if _, ok := l.Acquire(context.Background(), "other", 1); !ok {
		t.Fatal("a fresh key should have its own budget")
	}
}

// TestGraceLetsAWaiterThrough: a slot freed within the grace window admits the
// waiting caller instead of 503-ing it (CoverGo U18).
func TestGraceLetsAWaiterThrough(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := New(time.Second)
		rel, _ := l.Acquire(context.Background(), "up", 1)

		got := make(chan bool, 1)
		go func() {
			_, ok := l.Acquire(context.Background(), "up", 1)
			got <- ok
		}()

		synctest.Wait()   // the waiter is parked inside Acquire
		rel()             // free the slot within the grace window
		synctest.Wait()
		if !<-got {
			t.Fatal("waiter should have been admitted once the slot freed")
		}
	})
}
