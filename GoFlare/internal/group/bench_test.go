package group

import (
	"strconv"
	"testing"

	"github.com/levelcodingdev/goflare/internal/event"
)

// CoverGo U1 — the synchronous core of the ingest hot path: fingerprint an event
// and fold it into the group store (dedup → issue upsert → event append).
// The async Pipeline wraps this; measuring the core keeps the number stable.

func benchEvent(i int) event.Event {
	f := event.Frame{Module: "app.billing", Function: "charge", InApp: true, Lineno: 42}
	return event.Event{
		EventID:    "e-" + strconv.Itoa(i),
		Message:    "payment provider timeout",
		Level:      event.LevelError,
		Exceptions: []event.Exception{{Type: "GatewayError", Value: "upstream timed out", Frames: []event.Frame{f}}},
	}
}

func BenchmarkFingerprint(b *testing.B) {
	e := benchEvent(0)
	b.ReportAllocs()
	for b.Loop() {
		_ = Hash(Fingerprint(e))
	}
}

// BenchmarkIngestSameIssue: every event folds into one existing issue (the
// steady-state case — an error that fires repeatedly).
func BenchmarkIngestSameIssue(b *testing.B) {
	s := NewStore(50)
	e := benchEvent(0)
	s.Ingest("proj1", e) // create the issue
	b.ReportAllocs()
	for b.Loop() {
		s.Ingest("proj1", e)
	}
}

// BenchmarkIngestNewIssues: every event is a distinct fingerprint (the storm
// case — a deploy that breaks many code paths at once).
func BenchmarkIngestNewIssues(b *testing.B) {
	s := NewStore(50)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		e := event.Event{
			EventID:    "e-" + strconv.Itoa(i),
			Exceptions: []event.Exception{{Type: "E" + strconv.Itoa(i), Value: "v"}},
		}
		s.Ingest("proj1", e)
		i++
	}
}
