// Package project is GoFlare's registry of projects and their DSN keys. A
// project is the unit an SDK reports to and the unit the dashboard groups
// issues under. The in-memory Store here stands in for the Postgres-backed
// store planned in future.md (Phase 1).
package project

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrExists is returned when a slug is already taken.
	ErrExists = errors.New("project: already exists")
	// ErrNotFound is returned when a project or key is missing.
	ErrNotFound = errors.New("project: not found")
	// ErrAuth is returned when a (project, public key) pair does not match.
	ErrAuth = errors.New("project: bad DSN key")
	// ErrInvalid is returned when a project fails validation.
	ErrInvalid = errors.New("project: invalid")
)

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Key is one DSN public key for a project. SDKs authenticate ingest with it;
// it is not a secret in the sense an API token is (it ships in client code).
type Key struct {
	PublicKey string    `json:"public_key"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// Project is a reporting target.
type Project struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	TeamID    string    `json:"team_id,omitempty"` // owning team (Phase 1); "" = unassigned
	Keys      []Key     `json:"keys"`
	CreatedAt time.Time `json:"created_at"`

	seq int64 // insertion order, for a stable newest-first List
}

// DSN renders the client DSN for the project's first key against the given
// public base URL (scheme://host[:port]).
func (p Project) DSN(base string) string {
	if len(p.Keys) == 0 {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s@%s/%s", u.Scheme, p.Keys[0].PublicKey, u.Host, p.ID)
}

// Slugify turns a name into a URL-safe slug.
func Slugify(name string) string {
	s := slugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	return strings.Trim(s, "-")
}
