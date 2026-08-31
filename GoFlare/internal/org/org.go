// Package org is GoFlare's tenancy hierarchy: an organization owns teams, a
// team owns projects (internal/project gains a TeamID in Phase 1). It is the
// scope RBAC (Phase 7) and plan gating (Phase 7) attach to. The Store contract
// has an in-memory implementation here and a Postgres one in pgstore.go.
package org

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/levelcodingdev/goflare/internal/project"
)

var (
	// ErrExists is returned when an org or team slug is already taken.
	ErrExists = errors.New("org: already exists")
	// ErrNotFound is returned when an org or team is missing.
	ErrNotFound = errors.New("org: not found")
	// ErrInvalid is returned on failed validation.
	ErrInvalid = errors.New("org: invalid")
)

// Org is the top-level tenant.
type Org struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Team groups projects within an org.
type Team struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is the tenancy persistence contract.
type Store interface {
	CreateOrg(name string) (Org, error)
	GetOrg(id string) (Org, error)
	ListOrgs() []Org

	CreateTeam(orgID, name string) (Team, error)
	GetTeam(id string) (Team, error)
	ListTeams(orgID string) []Team
}

// normName trims a display name and derives its slug, or returns ErrInvalid.
func normName(name string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("%w: name is required", ErrInvalid)
	}
	slug := project.Slugify(name)
	if slug == "" {
		return "", "", fmt.Errorf("%w: name has no slug-able characters", ErrInvalid)
	}
	return name, slug, nil
}
