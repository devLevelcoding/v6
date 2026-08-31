package group

import (
	"errors"
	"testing"
	"time"

	"github.com/levelcodingdev/goflare/internal/event"
)

// errEvent builds an event whose grouping is pinned to typ (via a stable in-app
// frame), so tests can vary the value freely and still land on one issue.
func errEvent(typ, val string) event.Event {
	return event.Event{
		Level:     event.LevelError,
		Timestamp: time.Now(),
		Exceptions: []event.Exception{{
			Type:   typ,
			Value:  val,
			Frames: []event.Frame{{Module: "app." + typ, Function: "run", InApp: true}},
		}},
	}
}

func TestIngestNewThenRecurring(t *testing.T) {
	s := NewStore(10)

	iss, outcome := s.Ingest("p1", errEvent("Boom", "1"))
	if outcome != OutcomeNew || iss.TimesSeen != 1 || iss.Status != StatusUnresolved {
		t.Fatalf("first ingest: %s %+v", outcome, iss)
	}

	iss2, outcome := s.Ingest("p1", errEvent("Boom", "2"))
	if outcome != OutcomeRecurring || iss2.ID != iss.ID || iss2.TimesSeen != 2 {
		t.Fatalf("second ingest: %s %+v", outcome, iss2)
	}
}

func TestIngestRegression(t *testing.T) {
	s := NewStore(10)
	iss, _ := s.Ingest("p1", errEvent("Boom", "x"))
	if _, err := s.SetStatus(iss.ID, StatusResolved); err != nil {
		t.Fatal(err)
	}

	got, outcome := s.Ingest("p1", errEvent("Boom", "y"))
	if outcome != OutcomeRegression {
		t.Fatalf("outcome = %s, want regression", outcome)
	}
	if got.Status != StatusUnresolved || !got.Regressed {
		t.Fatalf("regressed issue = %+v", got)
	}

	// Acknowledging by setting unresolved clears the flag.
	cleared, _ := s.SetStatus(iss.ID, StatusUnresolved)
	if cleared.Regressed {
		t.Error("Regressed should clear when moved to unresolved")
	}
}

func TestIngestPerProjectIsolation(t *testing.T) {
	s := NewStore(10)
	a, _ := s.Ingest("p1", errEvent("Same", "x"))
	b, _ := s.Ingest("p2", errEvent("Same", "x"))
	if a.ID == b.ID {
		t.Error("identical errors in different projects must not share an issue")
	}
}

func TestListFilters(t *testing.T) {
	s := NewStore(10)
	open, _ := s.Ingest("p1", errEvent("OpenErr", "x"))
	done, _ := s.Ingest("p1", errEvent("DoneErr", "x"))
	s.Ingest("p2", errEvent("OtherProj", "x"))
	s.SetStatus(done.ID, StatusResolved)

	if got := s.List(Filter{ProjectID: "p1"}); len(got) != 2 {
		t.Fatalf("p1 issues = %d", len(got))
	}
	if got := s.List(Filter{ProjectID: "p1", Status: StatusUnresolved}); len(got) != 1 || got[0].ID != open.ID {
		t.Fatalf("unresolved filter = %+v", got)
	}
	if got := s.List(Filter{ProjectID: "p1", Query: "doneerr"}); len(got) != 1 || got[0].ID != done.ID {
		t.Fatalf("query filter = %+v", got)
	}
}

func TestEventsSampleNewestFirstAndCap(t *testing.T) {
	s := NewStore(3)
	var id string
	for i := 0; i < 5; i++ {
		e := errEvent("Boom", "x")
		e.Message = string(rune('a' + i))
		iss, _ := s.Ingest("p1", e)
		id = iss.ID
		time.Sleep(time.Millisecond)
	}
	evs, err := s.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("sample size = %d, want 3", len(evs))
	}
	if !evs[0].Received.After(evs[1].Received) {
		t.Error("events not newest-first")
	}

	iss, _ := s.Get(id)
	if iss.TimesSeen != 5 {
		t.Errorf("TimesSeen = %d, want 5 (count survives sampling)", iss.TimesSeen)
	}

	latest, err := s.LatestEvent(id)
	if err != nil || latest.Received.Before(evs[1].Received) {
		t.Errorf("LatestEvent wrong: %+v %v", latest, err)
	}
}

func TestSetStatusValidation(t *testing.T) {
	s := NewStore(10)
	iss, _ := s.Ingest("p1", errEvent("Boom", "x"))
	if _, err := s.SetStatus(iss.ID, "bogus"); err == nil {
		t.Error("expected invalid status error")
	}
	if _, err := s.SetStatus("missing", StatusResolved); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
