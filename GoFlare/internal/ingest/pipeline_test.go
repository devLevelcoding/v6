package ingest

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"testing/synctest"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func ev(msg string) event.Event {
	return event.Event{Message: msg, Level: event.LevelError, EventID: "e-" + msg}
}

func TestPipelineDrainsToGroupStore(t *testing.T) {
	// synctest (CoverGo U24): no polling loop — synctest.Wait blocks until all
	// three workers are parked back on <-p.ch, i.e. every event is grouped.
	synctest.Test(t, func(t *testing.T) {
		groups := group.NewStore(50)
		p := NewPipeline(groups, 3, 100, quiet())

		ctx, cancel := context.WithCancel(context.Background())
		p.Start(ctx)

		for i := 0; i < 40; i++ {
			if err := p.Submit("proj1", "proj1", ev("boom")); err != nil {
				t.Fatalf("Submit %d: %v", i, err)
			}
		}
		synctest.Wait() // workers idle again ⇒ queue drained

		cancel()
		if err := p.Wait(); err != nil {
			t.Fatalf("pipeline exited with error: %v", err)
		}
		iss := groups.List(group.Filter{ProjectID: "proj1"})
		if len(iss) != 1 || iss[0].TimesSeen != 40 {
			t.Fatalf("pipeline did not group all 40 events: %+v", iss)
		}
	})
}

// TestPipelineNoGoroutineLeak (CoverGo U19): every worker goroutine is gone
// once Wait() returns. synctest makes the check exact.
func TestPipelineNoGoroutineLeak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		before := runtime.NumGoroutine()

		groups := group.NewStore(50)
		p := NewPipeline(groups, 4, 100, quiet())
		ctx, cancel := context.WithCancel(context.Background())
		p.Start(ctx)
		for i := 0; i < 30; i++ {
			_ = p.Submit("p", "p", ev("x"))
		}
		synctest.Wait()
		cancel()
		if err := p.Wait(); err != nil {
			t.Fatalf("pipeline: %v", err)
		}
		synctest.Wait()

		if after := runtime.NumGoroutine(); after != before {
			t.Fatalf("goroutine leak: %d before, %d after Wait()", before, after)
		}
	})
}

func TestPipelineBackpressure(t *testing.T) {
	groups := group.NewStore(10)
	// tiny queue, no workers started → it fills and stays full
	p := NewPipeline(groups, 1, 2, quiet())

	if err := p.Submit("p", "p", ev("a")); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if err := p.Submit("p", "p", ev("b")); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if err := p.Submit("p", "p", ev("c")); err != ErrBusy {
		t.Fatalf("third submit into a full queue = %v, want ErrBusy", err)
	}
	if p.Depth() != 2 {
		t.Fatalf("Depth = %d, want 2", p.Depth())
	}
}

func TestPipelineFlushesOnShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		groups := group.NewStore(50)
		p := NewPipeline(groups, 2, 50, quiet())
		ctx, cancel := context.WithCancel(context.Background())
		p.Start(ctx)

		for i := 0; i < 20; i++ {
			_ = p.Submit("p", "p", ev("x"))
		}
		cancel() // workers drain what's queued before returning
		if err := p.Wait(); err != nil {
			t.Fatalf("pipeline exited with error: %v", err)
		}

		iss := groups.List(group.Filter{ProjectID: "p"})
		if len(iss) != 1 || iss[0].TimesSeen != 20 {
			t.Fatalf("shutdown flush lost events: %+v", iss)
		}
	})
}
