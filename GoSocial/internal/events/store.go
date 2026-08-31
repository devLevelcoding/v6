// Package events is an append-only, optimistic-concurrency event store.
// Posts and follows are modeled as events appended to one stream, and all
// read state is a projection computed by replaying that stream (see
// social.go for the projector and replay.go for point-in-time replay).
package events

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrConcurrencyConflict is returned when optimistic concurrency check fails.
var ErrConcurrencyConflict = errors.New("concurrency conflict: version mismatch")

// ErrStreamNotFound is returned when a stream does not exist.
var ErrStreamNotFound = errors.New("stream not found")

// EventRecord is a single persisted event in the store.
type EventRecord struct {
	StreamID   string
	Version    int
	EventType  string
	Payload    []byte
	OccurredAt time.Time
}

// EventStore persists and loads a stream's events.
type EventStore interface {
	AppendEvents(streamID string, expectedVersion int, events []EventRecord) error
	LoadEvents(streamID string) ([]EventRecord, error)
	LoadEventsFrom(streamID string, fromVersion int) ([]EventRecord, error)
}

// InMemoryEventStore is a thread-safe, in-memory EventStore.
type InMemoryEventStore struct {
	mu      sync.RWMutex
	streams map[string][]EventRecord
}

// NewInMemoryEventStore creates a new InMemoryEventStore.
func NewInMemoryEventStore() *InMemoryEventStore {
	return &InMemoryEventStore{
		streams: make(map[string][]EventRecord),
	}
}

// AppendEvents appends events to a stream with optimistic concurrency control.
func (s *InMemoryEventStore) AppendEvents(streamID string, expectedVersion int, events []EventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.streams[streamID]
	currentVersion := len(existing) - 1 // -1 if empty

	if currentVersion != expectedVersion {
		return fmt.Errorf("%w: expected %d, got %d", ErrConcurrencyConflict, expectedVersion, currentVersion)
	}

	nextVersion := len(existing)
	for i, ev := range events {
		ev.StreamID = streamID
		ev.Version = nextVersion + i
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = time.Now()
		}
		existing = append(existing, ev)
	}

	s.streams[streamID] = existing
	return nil
}

// LoadEvents returns all events for the given stream.
func (s *InMemoryEventStore) LoadEvents(streamID string) ([]EventRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events, ok := s.streams[streamID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrStreamNotFound, streamID)
	}

	result := make([]EventRecord, len(events))
	copy(result, events)
	return result, nil
}

// LoadEventsFrom returns events starting from fromVersion (inclusive).
func (s *InMemoryEventStore) LoadEventsFrom(streamID string, fromVersion int) ([]EventRecord, error) {
	all, err := s.LoadEvents(streamID)
	if err != nil {
		return nil, err
	}

	if fromVersion >= len(all) {
		return []EventRecord{}, nil
	}

	result := make([]EventRecord, len(all)-fromVersion)
	copy(result, all[fromVersion:])
	return result, nil
}

// AllStreamIDs returns every stream ID currently known to the store.
func (s *InMemoryEventStore) AllStreamIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.streams))
	for id := range s.streams {
		out = append(out, id)
	}
	return out
}
