package incident

import (
	"testing"
	"time"

	"github.com/levelcodingdev/gouptime/internal/check"
)

func res(id string, ok bool, at time.Time) check.Result {
	return check.Result{MonitorID: id, OK: ok, At: at, Detail: "detail"}
}

func TestDetectorOpensAfterThreshold(t *testing.T) {
	d := NewDetector(Policy{FailThreshold: 3, RecoverThreshold: 2})
	base := time.Now()

	for i := 0; i < 2; i++ {
		if _, changed := d.Observe(res("m1", false, base.Add(time.Duration(i)*time.Second))); changed {
			t.Fatalf("opened early at failure %d", i+1)
		}
	}
	ev, changed := d.Observe(res("m1", false, base.Add(2*time.Second)))
	if !changed || ev.Type != Opened {
		t.Fatalf("expected Opened, got %+v changed=%v", ev, changed)
	}
	if ev.Incident.MonitorID != "m1" || ev.Incident.Cause != "detail" {
		t.Errorf("incident fields wrong: %+v", ev.Incident)
	}
	if _, ok := d.OpenIncident("m1"); !ok {
		t.Error("OpenIncident should report the open incident")
	}
}

func TestDetectorResolvesAfterRecovery(t *testing.T) {
	d := NewDetector(Policy{FailThreshold: 2, RecoverThreshold: 2})
	base := time.Now()

	d.Observe(res("m1", false, base))
	d.Observe(res("m1", false, base.Add(time.Second))) // opens

	if _, changed := d.Observe(res("m1", true, base.Add(2*time.Second))); changed {
		t.Fatal("resolved after a single success")
	}
	ev, changed := d.Observe(res("m1", true, base.Add(3*time.Second)))
	if !changed || ev.Type != Resolved {
		t.Fatalf("expected Resolved, got %+v changed=%v", ev, changed)
	}
	if ev.Incident.ResolvedAt == nil || ev.Incident.Open() {
		t.Errorf("resolved incident still open: %+v", ev.Incident)
	}
	if _, ok := d.OpenIncident("m1"); ok {
		t.Error("OpenIncident should be empty after resolve")
	}
}

func TestDetectorBlipDoesNotOpen(t *testing.T) {
	d := NewDetector(DefaultPolicy())
	base := time.Now()
	d.Observe(res("m1", false, base))
	d.Observe(res("m1", true, base.Add(time.Second)))
	for i := 0; i < 2; i++ {
		if _, changed := d.Observe(res("m1", false, base.Add(time.Duration(i+2)*time.Second))); changed {
			t.Fatal("a single failure before recovery should reset the streak")
		}
	}
}

func TestDetectorCountsFailuresDuringIncident(t *testing.T) {
	d := NewDetector(Policy{FailThreshold: 1, RecoverThreshold: 1})
	base := time.Now()
	d.Observe(res("m1", false, base)) // opens, FailCount 1
	d.Observe(res("m1", false, base.Add(time.Second)))
	d.Observe(res("m1", false, base.Add(2*time.Second)))
	d.Observe(res("m1", true, base.Add(3*time.Second))) // resolves

	incs := d.Incidents("m1")
	if len(incs) != 1 {
		t.Fatalf("want 1 incident, got %d", len(incs))
	}
	if incs[0].FailCount != 3 {
		t.Errorf("FailCount = %d, want 3", incs[0].FailCount)
	}
}

func TestDetectorPerMonitorIsolation(t *testing.T) {
	d := NewDetector(Policy{FailThreshold: 1, RecoverThreshold: 1})
	now := time.Now()
	d.Observe(res("m1", false, now))
	d.Observe(res("m2", true, now))

	if len(d.Incidents("m1")) != 1 {
		t.Error("m1 should have an incident")
	}
	if len(d.Incidents("m2")) != 0 {
		t.Error("m2 should have no incident")
	}
	if len(d.Incidents("")) != 1 {
		t.Error("unfiltered should return all incidents")
	}
}
