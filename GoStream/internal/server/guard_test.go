package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/levelcodingdev/gostream/internal/hub"
)

// TestGuardContainsPanic (CoverGo P3): a panic in a connection loop is recovered,
// the client is killed (so the sibling loop unblocks), and the panic does not
// propagate to crash the process.
func TestGuardContainsPanic(t *testing.T) {
	s := &server{cfg: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	h := hub.New(hub.Config{})
	c := h.Add(hub.Meta{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.guard("writeLoop", c, func() { panic("boom") })
	}()
	<-done

	select {
	case <-c.Done():
		// killed, as expected
	default:
		t.Fatal("guard should have killed the client after a panic")
	}
	if c.KillReason() != "panic in writeLoop" {
		t.Fatalf("kill reason = %q", c.KillReason())
	}
}
