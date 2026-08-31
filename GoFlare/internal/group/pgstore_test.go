package group_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/levelcodingdev/goflare/internal/blob"
	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/pgtest"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func errEvent(msg, culprit string, lvl event.Level) event.Event {
	e := event.Event{
		Message: msg,
		Level:   lvl,
		Exceptions: []event.Exception{{
			Type:  "RuntimeError",
			Value: msg,
			Frames: []event.Frame{
				{Function: "handler", Filename: culprit, InApp: true},
			},
		}},
	}
	return e
}

func TestPGGroupStore(t *testing.T) {
	db := pgtest.DB(t)
	blobs := blob.NewMemStore()
	s := group.NewStore(5)
	if err := s.UsePostgres(db, blobs, quietLog()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// first event → new issue
	iss, oc := s.Ingest("proj1", errEvent("boom", "app/pay.go", event.LevelError))
	if oc != group.OutcomeNew || iss.ID == "" || iss.TimesSeen != 1 {
		t.Fatalf("first ingest: outcome=%v issue=%+v", oc, iss)
	}
	firstID := iss.ID

	// same fingerprint → recurring, times_seen grows, worst level wins
	iss, oc = s.Ingest("proj1", errEvent("boom", "app/pay.go", event.LevelFatal))
	if oc != group.OutcomeRecurring || iss.ID != firstID || iss.TimesSeen != 2 {
		t.Fatalf("recurring ingest: outcome=%v issue=%+v", oc, iss)
	}
	if iss.Level != event.LevelFatal {
		t.Fatalf("level should escalate to fatal, got %q", iss.Level)
	}

	// different fingerprint → separate issue
	iss2, oc := s.Ingest("proj1", errEvent("nope", "app/user.go", event.LevelWarning))
	if oc != group.OutcomeNew || iss2.ID == firstID {
		t.Fatalf("second fingerprint should be a new issue: %+v %v", iss2, oc)
	}

	// project isolation
	if _, oc := s.Ingest("proj2", errEvent("boom", "app/pay.go", event.LevelError)); oc != group.OutcomeNew {
		t.Fatalf("same fingerprint under a different project must be new, got %v", oc)
	}

	// resolve then re-ingest → regression
	if _, err := s.SetStatus(firstID, group.StatusResolved); err != nil {
		t.Fatal(err)
	}
	iss, oc = s.Ingest("proj1", errEvent("boom", "app/pay.go", event.LevelError))
	if oc != group.OutcomeRegression || !iss.Regressed || iss.Status != group.StatusUnresolved {
		t.Fatalf("regression: outcome=%v issue=%+v", oc, iss)
	}

	// acknowledging (→ unresolved) clears the regression flag
	iss, _ = s.SetStatus(firstID, group.StatusUnresolved)
	if iss.Regressed {
		t.Fatal("regressed flag should clear when moved to unresolved")
	}

	// Get / List filters
	if _, err := s.Get(firstID); err != nil {
		t.Fatalf("Get(firstID) = %v", err)
	}
	if _, err := s.Get("ghost"); err != group.ErrNotFound {
		t.Fatalf("Get(ghost) = %v, want ErrNotFound", err)
	}
	if all := s.List(group.Filter{ProjectID: "proj1"}); len(all) != 2 {
		t.Fatalf("List(proj1) = %d, want 2", len(all))
	}
	if byQ := s.List(group.Filter{ProjectID: "proj1", Query: "user.go"}); len(byQ) != 1 {
		t.Fatalf("List(query user.go) = %d, want 1", len(byQ))
	}

	// Events: PG keeps ALL of them, not just the bounded sample of 5
	for i := 0; i < 12; i++ {
		s.Ingest("proj1", errEvent("boom", "app/pay.go", event.LevelError))
	}
	evs, err := s.Events(firstID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < 12 {
		t.Fatalf("Events returned %d, expected all (>=12) — PG is not sample-bounded", len(evs))
	}
	if n, _ := s.PGEventCount("proj1"); n < 15 {
		t.Fatalf("PGEventCount(proj1) = %d, want >= 15", n)
	}

	// the raw bodies really landed in the blob store
	keys, _ := blobs.List(ctx, "events/proj1/")
	if len(keys) < 15 {
		t.Fatalf("blob store has %d proj1 event bodies, want >= 15", len(keys))
	}

	// newest-first ordering on Events
	le, err := s.LatestEvent(firstID)
	if err != nil || le.EventID == "" {
		t.Fatalf("LatestEvent = %+v, %v", le, err)
	}
}
