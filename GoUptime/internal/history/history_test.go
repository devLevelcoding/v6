package history

import (
	"testing"
	"time"

	"github.com/levelcodingdev/gouptime/internal/check"
)

func TestRingEvictsOldest(t *testing.T) {
	r := NewRing(3)
	base := time.Now()
	for i := 0; i < 5; i++ {
		r.Record(check.Result{MonitorID: "m1", At: base.Add(time.Duration(i) * time.Second), OK: true})
	}
	got := r.Results("m1", 0)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// newest first
	if !got[0].At.After(got[1].At) || !got[1].At.After(got[2].At) {
		t.Errorf("results not newest-first: %v", got)
	}
	if got[2].At != base.Add(2*time.Second) {
		t.Errorf("oldest retained = %v, want base+2s", got[2].At)
	}
}

func TestRingResultsLimit(t *testing.T) {
	r := NewRing(10)
	for i := 0; i < 10; i++ {
		r.Record(check.Result{MonitorID: "m1", At: time.Now().Add(time.Duration(i) * time.Second)})
	}
	if got := r.Results("m1", 4); len(got) != 4 {
		t.Fatalf("limit ignored: got %d", len(got))
	}
}

func TestRingSummary(t *testing.T) {
	r := NewRing(100)
	base := time.Now()
	for i := 0; i < 10; i++ {
		r.Record(check.Result{
			MonitorID: "m1",
			At:        base.Add(time.Duration(i) * time.Second),
			OK:        i%2 == 0, // 5 up (0,2,4,6,8), 5 down
			Latency:   100 * time.Millisecond,
		})
	}
	s := r.Summary("m1")
	if s.Total != 10 || s.Up != 5 {
		t.Fatalf("summary counts wrong: %+v", s)
	}
	if s.UptimeRatio != 0.5 {
		t.Errorf("UptimeRatio = %v, want 0.5", s.UptimeRatio)
	}
	if s.AvgLatency != 100*time.Millisecond {
		t.Errorf("AvgLatency = %v", s.AvgLatency)
	}
	if s.Last == nil || s.Last.At != base.Add(9*time.Second) {
		t.Errorf("Last wrong: %+v", s.Last)
	}
}

func TestRingSummaryEmpty(t *testing.T) {
	s := NewRing(10).Summary("nope")
	if s.Total != 0 || s.Last != nil {
		t.Errorf("empty summary wrong: %+v", s)
	}
}
