package org

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/levelcodingdev/goflare/internal/uid"
	"github.com/lib/pq"
)

// PGStore is a Postgres-backed Store.
type PGStore struct {
	db  *sql.DB
	now func() time.Time
}

// NewPGStore runs the migration and returns a store over db.
func NewPGStore(db *sql.DB) (*PGStore, error) {
	if err := Migrate(db); err != nil {
		return nil, err
	}
	return &PGStore{db: db, now: time.Now}, nil
}

// Migrate creates the orgs + teams tables.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS goflare.orgs (
			id         TEXT PRIMARY KEY,
			slug       TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS goflare.teams (
			id         TEXT PRIMARY KEY,
			org_id     TEXT NOT NULL REFERENCES goflare.orgs(id) ON DELETE CASCADE,
			slug       TEXT NOT NULL,
			name       TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (org_id, slug)
		);
		CREATE INDEX IF NOT EXISTS teams_org_idx ON goflare.teams(org_id);
	`)
	if err != nil {
		return fmt.Errorf("org: migrate: %w", err)
	}
	return nil
}

func (s *PGStore) CreateOrg(name string) (Org, error) {
	name, slug, err := normName(name)
	if err != nil {
		return Org{}, err
	}
	o := Org{ID: uid.New()[:16], Slug: slug, Name: name, CreatedAt: s.now().UTC()}
	_, err = s.db.Exec(
		`INSERT INTO goflare.orgs (id, slug, name, created_at) VALUES ($1,$2,$3,$4)`,
		o.ID, o.Slug, o.Name, o.CreatedAt)
	if err != nil {
		if isUnique(err) {
			return Org{}, ErrExists
		}
		return Org{}, err
	}
	return o, nil
}

func (s *PGStore) GetOrg(id string) (Org, error) {
	var o Org
	err := s.db.QueryRow(
		`SELECT id, slug, name, created_at FROM goflare.orgs WHERE id = $1`, id).
		Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Org{}, ErrNotFound
	}
	return o, err
}

func (s *PGStore) ListOrgs() []Org {
	rows, err := s.db.Query(`SELECT id, slug, name, created_at FROM goflare.orgs ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Org
	for rows.Next() {
		var o Org
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
			return out
		}
		out = append(out, o)
	}
	return out
}

func (s *PGStore) CreateTeam(orgID, name string) (Team, error) {
	name, slug, err := normName(name)
	if err != nil {
		return Team{}, err
	}
	if _, err := s.GetOrg(orgID); err != nil {
		return Team{}, err
	}
	t := Team{ID: uid.New()[:16], OrgID: orgID, Slug: slug, Name: name, CreatedAt: s.now().UTC()}
	_, err = s.db.Exec(
		`INSERT INTO goflare.teams (id, org_id, slug, name, created_at) VALUES ($1,$2,$3,$4,$5)`,
		t.ID, t.OrgID, t.Slug, t.Name, t.CreatedAt)
	if err != nil {
		if isUnique(err) {
			return Team{}, ErrExists
		}
		return Team{}, err
	}
	return t, nil
}

func (s *PGStore) GetTeam(id string) (Team, error) {
	var t Team
	err := s.db.QueryRow(
		`SELECT id, org_id, slug, name, created_at FROM goflare.teams WHERE id = $1`, id).
		Scan(&t.ID, &t.OrgID, &t.Slug, &t.Name, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Team{}, ErrNotFound
	}
	return t, err
}

func (s *PGStore) ListTeams(orgID string) []Team {
	var (
		rows *sql.Rows
		err  error
	)
	if orgID == "" {
		rows, err = s.db.Query(`SELECT id, org_id, slug, name, created_at FROM goflare.teams ORDER BY created_at`)
	} else {
		rows, err = s.db.Query(`SELECT id, org_id, slug, name, created_at FROM goflare.teams WHERE org_id = $1 ORDER BY created_at`, orgID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Slug, &t.Name, &t.CreatedAt); err != nil {
			return out
		}
		out = append(out, t)
	}
	return out
}

func isUnique(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
