// Package project is GoFlare's registry of projects and their DSN keys. A
// project is the unit an SDK reports to and the unit the dashboard groups
// issues under.
//
// One Store interface, two implementations: MemStore here (persisted by package
// snapshot via Snapshot/Restore in persist.go; Seed in seed.go) and PGStore in
// pgstore.go / pgread.go. The dashboard API and ingest hold the interface and
// don't know which.
package project

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/levelcodingdev/goflare/internal/uid"
)

// Store is the persistence contract used by ingest and the dashboard API.
type Store interface {
	Create(name, platform string) (Project, error)
	Get(id string) (Project, error)
	BySlug(slug string) (Project, error)
	List() []Project
	// Authenticate resolves the project for an ingest request and checks the
	// public key against it.
	Authenticate(projectID, publicKey string) (Project, error)
}

// MemStore is an in-memory Store, safe for concurrent use.
type MemStore struct {
	mu     sync.RWMutex
	now    func() time.Time
	seq    int64
	byID   map[string]*Project
	bySlug map[string]string // slug -> id
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{now: time.Now, byID: map[string]*Project{}, bySlug: map[string]string{}}
}

// put inserts a project under both indexes and returns a copy. The caller holds
// the write lock; id / publicKey must be non-empty.
func (s *MemStore) put(id, slug, name, platform, publicKey string) Project {
	s.seq++
	p := &Project{
		ID:        id,
		Slug:      slug,
		Name:      name,
		Platform:  strings.TrimSpace(platform),
		Keys:      []Key{{PublicKey: publicKey, Label: "default", CreatedAt: s.now()}},
		CreatedAt: s.now(),
		seq:       s.seq,
	}
	s.byID[p.ID] = p
	s.bySlug[slug] = p.ID
	return *p
}

// Create adds a project with one freshly generated DSN key.
func (s *MemStore) Create(name, platform string) (Project, error) {
	name, slug, err := nameAndSlug(name)
	if err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.bySlug[slug]; taken {
		return Project{}, ErrExists
	}
	return s.put(uid.New()[:16], slug, name, platform, uid.New()), nil
}

// Get returns one project by ID.
func (s *MemStore) Get(id string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byID[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	return *p, nil
}

// BySlug returns one project by slug.
func (s *MemStore) BySlug(slug string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.bySlug[slug]
	if !ok {
		return Project{}, ErrNotFound
	}
	return *s.byID[id], nil
}

// List returns all projects, newest first.
func (s *MemStore) List() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Project, 0, len(s.byID))
	for _, p := range s.byID {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq > out[j].seq })
	return out
}

// Authenticate checks that publicKey is a valid key for projectID.
func (s *MemStore) Authenticate(projectID, publicKey string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byID[projectID]
	if !ok {
		return Project{}, ErrNotFound
	}
	for _, k := range p.Keys {
		if k.PublicKey == publicKey {
			return *p, nil
		}
	}
	return Project{}, ErrAuth
}
