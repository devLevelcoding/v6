package project

import "sort"

// Snapshot returns every project in creation order — the serializable form used
// by package snapshot to persist the store across restarts. (MemStore only.)
func (s *MemStore) Snapshot() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Project, 0, len(s.byID))
	for _, p := range s.byID {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// Restore replaces the store's contents with projects (as returned by
// Snapshot). Insertion order is rebuilt from CreatedAt so List stays stable.
func (s *MemStore) Restore(projects []Project) {
	ordered := append([]Project(nil), projects...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CreatedAt.Before(ordered[j].CreatedAt) })

	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[string]*Project, len(ordered))
	s.bySlug = make(map[string]string, len(ordered))
	s.seq = 0
	for i := range ordered {
		p := ordered[i]
		s.seq++
		p.seq = s.seq
		s.byID[p.ID] = &p
		s.bySlug[p.Slug] = p.ID
	}
}
