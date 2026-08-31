package group

import (
	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/uid"
)

// Ingest groups an event into an issue, creating or updating it, and records
// the event in the issue's sample.
func (s *Store) Ingest(projectID string, e event.Event) (Issue, Outcome) {
	hash := Hash(Fingerprint(e))
	if e.Timestamp.IsZero() {
		e.Timestamp = s.now()
	}
	e.ProjectID = projectID
	e.Received = s.now()

	if s.db != nil {
		iss, outcome, err := s.pgIngest(projectID, hash, e)
		if err != nil {
			// Ingest has no error return (it is called from the async worker
			// which logs); surface a best-effort issue so the caller's metrics
			// still tick. A durable-store failure is logged by pgIngest.
			return Issue{ProjectID: projectID, Hash: hash, Title: e.Title()}, OutcomeRecurring
		}
		return iss, outcome
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := projectID + "\x00" + hash
	id, exists := s.byHash[key]

	var (
		iss     *Issue
		outcome Outcome
	)
	if !exists {
		s.seq++
		iss = &Issue{
			seq:       s.seq,
			ID:        uid.New()[:16],
			ProjectID: projectID,
			Hash:      hash,
			Title:     e.Title(),
			Culprit:   e.Culprit(),
			Level:     e.Level,
			Platform:  e.Platform,
			Status:    StatusUnresolved,
			FirstSeen: e.Timestamp,
			LastSeen:  e.Timestamp,
		}
		s.byID[iss.ID] = iss
		s.byHash[key] = iss.ID
		outcome = OutcomeNew
	} else {
		iss = s.byID[id]
		switch iss.Status {
		case StatusResolved:
			iss.Status = StatusUnresolved
			iss.Regressed = true
			outcome = OutcomeRegression
		default:
			outcome = OutcomeRecurring
		}
	}

	iss.TimesSeen++
	if e.Timestamp.After(iss.LastSeen) {
		iss.LastSeen = e.Timestamp
	}
	if e.Timestamp.Before(iss.FirstSeen) {
		iss.FirstSeen = e.Timestamp
	}
	// The issue's headline tracks the latest event; its level is the worst seen.
	iss.Title = e.Title()
	if c := e.Culprit(); c != "" {
		iss.Culprit = c
	}
	if e.Platform != "" {
		iss.Platform = e.Platform
	}
	if levelRank(e.Level) > levelRank(iss.Level) {
		iss.Level = e.Level
	}

	buf := s.events[iss.ID]
	if len(buf) >= s.eventCap {
		buf = buf[len(buf)-s.eventCap+1:]
	}
	s.events[iss.ID] = append(buf, e)

	s.changed()
	return *iss, outcome
}

// Events returns an issue's sampled events, newest first, capped at limit
// (0 = all sampled).
func (s *Store) Events(id string, limit int) ([]event.Event, error) {
	if s.db != nil {
		return s.pgEvents(id, limit)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return nil, ErrNotFound
	}
	// The sample is stored in arrival order; newest-first is its reverse.
	src := s.events[id]
	out := make([]event.Event, len(src))
	for i, e := range src {
		out[len(src)-1-i] = e
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// LatestEvent returns the most recently received event for an issue.
func (s *Store) LatestEvent(id string) (event.Event, error) {
	evs, err := s.Events(id, 1)
	if err != nil {
		return event.Event{}, err
	}
	if len(evs) == 0 {
		return event.Event{}, ErrNotFound
	}
	return evs[0], nil
}
