package project

import (
	"fmt"

	"github.com/levelcodingdev/goflare/internal/uid"
)

// Seed is Create with a caller-chosen id and/or public key, so a deployment can
// know a project's DSN before the server first boots (put the id + key in
// config/secrets, pass them here, hand the rendered DSN to every client). An
// empty id or key falls back to a generated one. Idempotent: if a project with
// this slug already exists it is returned unchanged.
func (s *MemStore) Seed(name, platform, id, publicKey string) (Project, error) {
	name, slug, err := nameAndSlug(name)
	if err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID, taken := s.bySlug[slug]; taken {
		return *s.byID[existingID], nil
	}
	if id == "" {
		id = uid.New()[:16]
	}
	if _, clash := s.byID[id]; clash {
		return Project{}, fmt.Errorf("%w: project id already in use", ErrExists)
	}
	if publicKey == "" {
		publicKey = uid.New()
	}
	return s.put(id, slug, name, platform, publicKey), nil
}
