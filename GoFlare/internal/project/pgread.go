package project

import (
	"database/sql"
	"errors"
)

const projectCols = `p.id, p.slug, p.name, p.platform, COALESCE(p.team_id,''), p.created_at`

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.Platform, &p.TeamID, &p.CreatedAt)
	return p, err
}

// Get returns one project by id, keys included.
func (s *PGStore) Get(id string) (Project, error) { return s.queryOne(`p.id = $1`, id) }

// BySlug returns one project by slug.
func (s *PGStore) BySlug(slug string) (Project, error) { return s.queryOne(`p.slug = $1`, slug) }

func (s *PGStore) queryOne(where string, arg any) (Project, error) {
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM goflare.projects p WHERE `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	if p.Keys, err = s.keysFor(p.ID); err != nil {
		return Project{}, err
	}
	return p, nil
}

func (s *PGStore) keysFor(projectID string) ([]Key, error) {
	rows, err := s.db.Query(
		`SELECT public_key, label, created_at FROM goflare.dsn_keys WHERE project_id = $1 ORDER BY created_at`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []Key
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.PublicKey, &k.Label, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// List returns all projects, newest first.
func (s *PGStore) List() []Project {
	rows, err := s.db.Query(`SELECT ` + projectCols + ` FROM goflare.projects p ORDER BY p.created_at DESC, p.id DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return out
		}
		out = append(out, p)
	}
	for i := range out {
		out[i].Keys, _ = s.keysFor(out[i].ID)
	}
	return out
}

// Authenticate checks that publicKey is a live key for projectID.
func (s *PGStore) Authenticate(projectID, publicKey string) (Project, error) {
	var ok bool
	err := s.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM goflare.dsn_keys WHERE project_id=$1 AND public_key=$2)`,
		projectID, publicKey).Scan(&ok)
	if err != nil {
		return Project{}, err
	}
	if !ok {
		// Distinguish "no such project" from "wrong key" for a better 401/404.
		if _, gerr := s.Get(projectID); errors.Is(gerr, ErrNotFound) {
			return Project{}, ErrNotFound
		}
		return Project{}, ErrAuth
	}
	return s.Get(projectID)
}
