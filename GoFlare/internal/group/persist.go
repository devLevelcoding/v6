package group

import (
	"sort"

	"github.com/levelcodingdev/goflare/internal/event"
)

// Snapshot is the serializable form of the store, used by package snapshot to
// persist issues and their sampled events across restarts. Only meaningful in
// the in-memory backend; empty / a no-op once Postgres-backed.
type Snapshot struct {
	Issues []Issue                  `json:"issues"`
	Events map[string][]event.Event `json:"events"`
}

// Snapshot returns a deep-enough copy of the store's state for persistence.
// Empty when Postgres-backed — durability is the database's job then.
func (s *Store) Snapshot() Snapshot {
	if s.db != nil {
		return Snapshot{Events: map[string][]event.Event{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	issues := make([]Issue, 0, len(s.byID))
	for _, iss := range s.byID {
		issues = append(issues, *iss)
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].seq < issues[j].seq })
	events := make(map[string][]event.Event, len(s.events))
	for id, buf := range s.events {
		events[id] = append([]event.Event(nil), buf...)
	}
	return Snapshot{Issues: issues, Events: events}
}

// Restore replaces the store's contents with a Snapshot. Issue creation order
// is rebuilt from FirstSeen so List stays stable across a reload. A no-op when
// Postgres-backed.
func (s *Store) Restore(snap Snapshot) {
	if s.db != nil {
		return
	}
	ordered := append([]Issue(nil), snap.Issues...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].FirstSeen.Before(ordered[j].FirstSeen) })

	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[string]*Issue, len(ordered))
	s.byHash = make(map[string]string, len(ordered))
	s.events = make(map[string][]event.Event, len(snap.Events))
	s.seq = 0
	for i := range ordered {
		iss := ordered[i]
		s.seq++
		iss.seq = s.seq
		s.byID[iss.ID] = &iss
		s.byHash[iss.ProjectID+"\x00"+iss.Hash] = iss.ID
	}
	for id, buf := range snap.Events {
		if _, ok := s.byID[id]; !ok {
			continue
		}
		if len(buf) > s.eventCap {
			buf = buf[len(buf)-s.eventCap:]
		}
		s.events[id] = append([]event.Event(nil), buf...)
	}
}
