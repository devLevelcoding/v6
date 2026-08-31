// Package incident turns a stream of check.Results into incidents. It applies a
// simple hysteresis policy — N consecutive failures opens an incident, M
// consecutive successes resolves it — so a single blip does not page anyone and
// a flapping target does not open dozens of incidents.
//
// This file holds the value types; the Detector state machine is in detector.go.
package incident

import "time"

// Policy is the open/resolve hysteresis.
type Policy struct {
	// FailThreshold is the number of consecutive failing probes that opens
	// an incident. Minimum 1.
	FailThreshold int
	// RecoverThreshold is the number of consecutive passing probes that
	// resolves an open incident. Minimum 1.
	RecoverThreshold int
}

// DefaultPolicy opens after 3 failures, resolves after 2 successes.
func DefaultPolicy() Policy { return Policy{FailThreshold: 3, RecoverThreshold: 2} }

func (p Policy) normalized() Policy {
	if p.FailThreshold < 1 {
		p.FailThreshold = 1
	}
	if p.RecoverThreshold < 1 {
		p.RecoverThreshold = 1
	}
	return p
}

// Incident is one continuous period of a monitor being down.
type Incident struct {
	ID         string     `json:"id"`
	MonitorID  string     `json:"monitor_id"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	// Cause is the failure detail that opened the incident.
	Cause string `json:"cause"`
	// FailCount is how many failing probes were seen during the incident.
	FailCount int `json:"fail_count"`
}

// Open reports whether the incident is still ongoing.
func (i Incident) Open() bool { return i.ResolvedAt == nil }

// EventType is "opened" or "resolved".
type EventType string

const (
	// Opened means a new incident was created by this result.
	Opened EventType = "opened"
	// Resolved means an open incident was closed by this result.
	Resolved EventType = "resolved"
)

// Event is emitted by Observe when incident state changes.
type Event struct {
	Type     EventType
	Incident Incident
}
