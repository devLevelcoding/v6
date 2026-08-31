package monitor

import (
	"sort"
	"sync"
	"time"

	"github.com/levelcodingdev/gouptime/internal/uid"
)

// Store is the persistence contract used by the API and scheduler.
type Store interface {
	Create(m Monitor) (Monitor, error)
	Get(id string) (Monitor, error)
	Update(m Monitor) (Monitor, error)
	Delete(id string) error
	List() []Monitor
}

// MemStore is an in-memory Store, safe for concurrent use.
type MemStore struct {
	mu  sync.RWMutex
	now func() time.Time
	m   map[string]Monitor
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{now: time.Now, m: map[string]Monitor{}}
}

// Create validates and inserts a monitor, assigning an ID if unset.
func (s *MemStore) Create(mon Monitor) (Monitor, error) {
	if err := mon.Validate(); err != nil {
		return Monitor{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mon.ID == "" {
		mon.ID = uid.New()
	}
	if _, ok := s.m[mon.ID]; ok {
		return Monitor{}, ErrExists
	}
	now := s.now()
	mon.CreatedAt, mon.UpdatedAt = now, now
	s.m[mon.ID] = mon
	return mon, nil
}

// Get returns one monitor by ID.
func (s *MemStore) Get(id string) (Monitor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mon, ok := s.m[id]
	if !ok {
		return Monitor{}, ErrNotFound
	}
	return mon, nil
}

// Update replaces an existing monitor, preserving CreatedAt.
func (s *MemStore) Update(mon Monitor) (Monitor, error) {
	if err := mon.Validate(); err != nil {
		return Monitor{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[mon.ID]
	if !ok {
		return Monitor{}, ErrNotFound
	}
	mon.CreatedAt = cur.CreatedAt
	mon.UpdatedAt = s.now()
	s.m[mon.ID] = mon
	return mon, nil
}

// Delete removes a monitor. Missing IDs return ErrNotFound.
func (s *MemStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return ErrNotFound
	}
	delete(s.m, id)
	return nil
}

// List returns all monitors, sorted by name then ID.
func (s *MemStore) List() []Monitor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Monitor, 0, len(s.m))
	for _, mon := range s.m {
		out = append(out, mon)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}
