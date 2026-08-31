package group

import (
	"errors"
	"time"

	"github.com/levelcodingdev/goflare/internal/event"
)

// ErrNotFound is returned when an issue id does not exist.
var ErrNotFound = errors.New("group: issue not found")

// Status is an issue's triage state.
type Status string

// Issue triage states.
const (
	StatusUnresolved Status = "unresolved"
	StatusResolved   Status = "resolved"
	StatusIgnored    Status = "ignored"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusUnresolved, StatusResolved, StatusIgnored:
		return true
	default:
		return false
	}
}

// Issue is a group of events that share a fingerprint.
type Issue struct {
	ID        string      `json:"id"`
	ProjectID string      `json:"project_id"`
	Hash      string      `json:"hash"`
	Title     string      `json:"title"`
	Culprit   string      `json:"culprit"`
	Level     event.Level `json:"level"`
	Platform  string      `json:"platform"`
	Status    Status      `json:"status"`
	FirstSeen time.Time   `json:"first_seen"`
	LastSeen  time.Time   `json:"last_seen"`
	TimesSeen int         `json:"times_seen"`
	// Regressed is set when a resolved issue received a new event.
	Regressed bool `json:"regressed"`

	seq int64 // creation order, for a stable sort under equal LastSeen
}

// Outcome is what Ingest did with an event.
type Outcome string

// Ingest outcomes.
const (
	OutcomeNew        Outcome = "new"        // first event for a fingerprint
	OutcomeRegression Outcome = "regression" // event landed on a resolved issue
	OutcomeRecurring  Outcome = "recurring"  // event on an already-open issue
)

// Filter narrows a List call.
type Filter struct {
	ProjectID string
	Status    Status // "" = any
	Query     string // substring match on title/culprit
}

func levelRank(l event.Level) int {
	switch l {
	case event.LevelFatal:
		return 5
	case event.LevelError:
		return 4
	case event.LevelWarning:
		return 3
	case event.LevelInfo:
		return 2
	case event.LevelDebug:
		return 1
	default:
		return 0
	}
}
