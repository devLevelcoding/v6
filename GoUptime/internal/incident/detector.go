package incident

import (
	"sort"
	"sync"

	"github.com/levelcodingdev/gouptime/internal/check"
	"github.com/levelcodingdev/gouptime/internal/uid"
)

type monState struct {
	consecFail int
	consecOK   int
	open       *Incident
}

// Detector holds per-monitor streak state and the incident log. Safe for
// concurrent use.
type Detector struct {
	mu     sync.Mutex
	policy Policy
	newID  func() string
	state  map[string]*monState
	log    []Incident
}

// NewDetector returns a Detector with the given policy.
func NewDetector(p Policy) *Detector {
	return &Detector{
		policy: p.normalized(),
		newID:  uid.New,
		state:  map[string]*monState{},
	}
}

// Observe feeds one result and reports a state change, if any.
func (d *Detector) Observe(r check.Result) (Event, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	st := d.state[r.MonitorID]
	if st == nil {
		st = &monState{}
		d.state[r.MonitorID] = st
	}

	if r.OK {
		st.consecOK++
		st.consecFail = 0
		if st.open != nil && st.consecOK >= d.policy.RecoverThreshold {
			resolved := r.At
			st.open.ResolvedAt = &resolved
			d.appendLocked(*st.open)
			ev := Event{Type: Resolved, Incident: *st.open}
			st.open = nil
			return ev, true
		}
		return Event{}, false
	}

	st.consecFail++
	st.consecOK = 0
	if st.open != nil {
		st.open.FailCount++
		return Event{}, false
	}
	if st.consecFail >= d.policy.FailThreshold {
		inc := &Incident{
			ID:        d.newID(),
			MonitorID: r.MonitorID,
			StartedAt: r.At,
			Cause:     r.Detail,
			FailCount: st.consecFail,
		}
		st.open = inc
		d.appendLocked(*inc)
		return Event{Type: Opened, Incident: *inc}, true
	}
	return Event{}, false
}

// appendLocked upserts an incident into the log by ID. Caller holds d.mu.
func (d *Detector) appendLocked(inc Incident) {
	for i := range d.log {
		if d.log[i].ID == inc.ID {
			d.log[i] = inc
			return
		}
	}
	d.log = append(d.log, inc)
}

// Incidents returns the incident log, newest first. If monitorID is non-empty
// it filters to that monitor.
func (d *Detector) Incidents(monitorID string) []Incident {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Incident, 0, len(d.log))
	for _, inc := range d.log {
		if monitorID == "" || inc.MonitorID == monitorID {
			out = append(out, inc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// OpenIncident returns the current open incident for a monitor, if any.
func (d *Detector) OpenIncident(monitorID string) (Incident, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if st := d.state[monitorID]; st != nil && st.open != nil {
		return *st.open, true
	}
	return Incident{}, false
}
