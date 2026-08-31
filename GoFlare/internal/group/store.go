// Package group turns events into issues: it computes a grouping fingerprint
// (fingerprint.go), upserts the issue an event belongs to and keeps a bounded
// sample of events per issue (ingest.go). This is the heart of a Sentry-style
// product — everything else is storage and presentation.
//
// Two backends behind one type (the GoAdmin gobase pattern): by default state
// is in memory and persisted by package snapshot (persist.go). After
// UsePostgres (pgstore.go) issues + an event index live in Postgres and raw
// event bodies in a blob.Store — the in-memory maps and the snapshot hooks go
// dormant. This file has the store type and its read/triage methods; Ingest and
// the event sample are in ingest.go, the value types in issue.go.
package group

import (
	"database/sql"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/levelcodingdev/goflare/internal/blob"
	"github.com/levelcodingdev/goflare/internal/event"
)

// Store holds issues and a bounded sample of each issue's events.
type Store struct {
	mu       sync.Mutex
	now      func() time.Time
	eventCap int
	seq      int64
	byID     map[string]*Issue
	byHash   map[string]string // projectID+"\x00"+hash -> issue id
	events   map[string][]event.Event
	onChange func() // optional; fired after any mutation (used for snapshotting)

	db    *sql.DB      // non-nil after UsePostgres
	blobs blob.Store   // non-nil after UsePostgres — raw event payloads
	log   *slog.Logger // pg-path error logging
}

// NewStore returns an empty store keeping up to eventsPerIssue events each.
func NewStore(eventsPerIssue int) *Store {
	if eventsPerIssue < 1 {
		eventsPerIssue = 1
	}
	return &Store{
		now:      time.Now,
		eventCap: eventsPerIssue,
		byID:     map[string]*Issue{},
		byHash:   map[string]string{},
		events:   map[string][]event.Event{},
	}
}

// SetOnChange registers a callback fired after any state mutation (an ingest or
// a status change). Used to mark a snapshot dirty. Not safe to call concurrently
// with ingest; set it once at wiring time.
func (s *Store) SetOnChange(fn func()) { s.onChange = fn }

func (s *Store) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

// Get returns one issue.
func (s *Store) Get(id string) (Issue, error) {
	if s.db != nil {
		return s.pgGet(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iss, ok := s.byID[id]
	if !ok {
		return Issue{}, ErrNotFound
	}
	return *iss, nil
}

// List returns issues matching the filter, most recently seen first.
func (s *Store) List(f Filter) []Issue {
	if s.db != nil {
		return s.pgList(f)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(f.Query)
	out := make([]Issue, 0, len(s.byID))
	for _, iss := range s.byID {
		if f.ProjectID != "" && iss.ProjectID != f.ProjectID {
			continue
		}
		if f.Status != "" && iss.Status != f.Status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(iss.Title), q) && !strings.Contains(strings.ToLower(iss.Culprit), q) {
			continue
		}
		out = append(out, *iss)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].seq > out[j].seq
	})
	return out
}

// SetStatus changes an issue's triage state. Moving to unresolved clears the
// regression flag (the user has acknowledged it).
func (s *Store) SetStatus(id string, status Status) (Issue, error) {
	if !status.Valid() {
		return Issue{}, errors.New("group: invalid status")
	}
	if s.db != nil {
		return s.pgSetStatus(id, status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iss, ok := s.byID[id]
	if !ok {
		return Issue{}, ErrNotFound
	}
	iss.Status = status
	if status == StatusUnresolved {
		iss.Regressed = false
	}
	s.changed()
	return *iss, nil
}
