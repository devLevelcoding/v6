package queue

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestPushClaimFIFO(t *testing.T) {
	q := NewMem(4)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := q.Push(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if q.Len() != 3 {
		t.Fatalf("Len = %d, want 3", q.Len())
	}
	for _, want := range []string{"a", "b", "c"} {
		got, ok := q.Claim(ctx)
		if !ok || got != want {
			t.Fatalf("Claim = %q,%v want %q,true", got, ok, want)
		}
	}
}

func TestClaimBlocksThenReceives(t *testing.T) {
	// synctest (CoverGo U24): deterministic — no timing guards.
	synctest.Test(t, func(t *testing.T) {
		q := NewMem(1)
		got := make(chan string, 1)
		go func() {
			id, _ := q.Claim(context.Background())
			got <- id
		}()

		synctest.Wait() // the Claim goroutine is now parked on <-q.ch
		select {
		case <-got:
			t.Fatal("Claim returned before anything was pushed")
		default:
		}

		_ = q.Push(context.Background(), "x")
		synctest.Wait()
		select {
		case id := <-got:
			if id != "x" {
				t.Fatalf("got %q, want x", id)
			}
		default:
			t.Fatal("Claim did not unblock after Push")
		}
	})
}

func TestClaimCtxCancel(t *testing.T) {
	q := NewMem(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := q.Claim(ctx); ok {
		t.Fatal("Claim should return false on cancelled ctx")
	}
}

func TestPushCtxCancelWhenFull(t *testing.T) {
	// synctest (CoverGo U24): virtual time fires the deadline instantly and
	// exactly, instead of a real 20ms wall-clock wait.
	synctest.Test(t, func(t *testing.T) {
		q := NewMem(1)
		_ = q.Push(context.Background(), "a") // fills buffer
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		start := time.Now()
		if err := q.Push(ctx, "b"); err == nil {
			t.Fatal("Push into full queue should fail once ctx expires")
		}
		if got := time.Since(start); got != 20*time.Millisecond {
			t.Fatalf("Push blocked for %v, want exactly 20ms", got)
		}
	})
}
