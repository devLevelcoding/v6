package org

import (
	"sort"
	"sync"
	"time"

	"github.com/levelcodingdev/goflare/internal/uid"
)

// MemStore is an in-memory Store, safe for concurrent use.
type MemStore struct {
	mu    sync.RWMutex
	now   func() time.Time
	seq   int64
	orgs  map[string]*Org
	teams map[string]*Team
	// slug uniqueness: org slugs are global, team slugs are per-org.
	orgSlug  map[string]string // slug -> org id
	teamSlug map[string]string // orgID+"\x00"+slug -> team id
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{
		now:      time.Now,
		orgs:     map[string]*Org{},
		teams:    map[string]*Team{},
		orgSlug:  map[string]string{},
		teamSlug: map[string]string{},
	}
}

func (s *MemStore) CreateOrg(name string) (Org, error) {
	name, slug, err := normName(name)
	if err != nil {
		return Org{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.orgSlug[slug]; taken {
		return Org{}, ErrExists
	}
	s.seq++
	o := &Org{ID: uid.New()[:16], Slug: slug, Name: name, CreatedAt: s.now()}
	s.orgs[o.ID] = o
	s.orgSlug[slug] = o.ID
	return *o, nil
}

func (s *MemStore) GetOrg(id string) (Org, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orgs[id]
	if !ok {
		return Org{}, ErrNotFound
	}
	return *o, nil
}

func (s *MemStore) ListOrgs() []Org {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Org, 0, len(s.orgs))
	for _, o := range s.orgs {
		out = append(out, *o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *MemStore) CreateTeam(orgID, name string) (Team, error) {
	name, slug, err := normName(name)
	if err != nil {
		return Team{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return Team{}, ErrNotFound
	}
	tkey := orgID + "\x00" + slug
	if _, taken := s.teamSlug[tkey]; taken {
		return Team{}, ErrExists
	}
	s.seq++
	t := &Team{ID: uid.New()[:16], OrgID: orgID, Slug: slug, Name: name, CreatedAt: s.now()}
	s.teams[t.ID] = t
	s.teamSlug[tkey] = t.ID
	return *t, nil
}

func (s *MemStore) GetTeam(id string) (Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.teams[id]
	if !ok {
		return Team{}, ErrNotFound
	}
	return *t, nil
}

func (s *MemStore) ListTeams(orgID string) []Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Team, 0)
	for _, t := range s.teams {
		if orgID == "" || t.OrgID == orgID {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
