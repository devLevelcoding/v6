package project

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/levelcodingdev/goflare/internal/uid"
	"github.com/lib/pq"
)

// PGStore is a Postgres-backed Store. It satisfies the same interface as
// MemStore; the dashboard API and ingest do not know which one they hold. This
// file has the write path (migrate + create + seed); reads are in pgread.go.
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

// Migrate creates the projects + dsn_keys tables.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS goflare.projects (
			id         TEXT PRIMARY KEY,
			slug       TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL,
			platform   TEXT NOT NULL DEFAULT '',
			team_id    TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS goflare.dsn_keys (
			public_key TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES goflare.projects(id) ON DELETE CASCADE,
			label      TEXT NOT NULL DEFAULT 'default',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS dsn_keys_project_idx ON goflare.dsn_keys(project_id);
		CREATE INDEX IF NOT EXISTS projects_team_idx ON goflare.projects(team_id);
	`)
	if err != nil {
		return fmt.Errorf("project: migrate: %w", err)
	}
	return nil
}

// Create adds a project with one generated DSN key.
func (s *PGStore) Create(name, platform string) (Project, error) {
	return s.CreateInTeam("", name, platform)
}

// CreateInTeam is Create with an owning team (Phase 1). teamID may be "".
func (s *PGStore) CreateInTeam(teamID, name, platform string) (Project, error) {
	name, slug, err := nameAndSlug(name)
	if err != nil {
		return Project{}, err
	}
	return s.insert(uid.New()[:16], teamID, slug, name, platform, uid.New())
}

// Seed is Create with a caller-chosen id and/or key, idempotent by slug — so a
// deployment can put the DSN in config before the server first boots.
func (s *PGStore) Seed(name, platform, id, publicKey string) (Project, error) {
	name, slug, err := nameAndSlug(name)
	if err != nil {
		return Project{}, err
	}
	if existing, err := s.BySlug(slug); err == nil {
		return existing, nil
	}
	if id == "" {
		id = uid.New()[:16]
	}
	if publicKey == "" {
		publicKey = uid.New()
	}
	return s.insert(id, "", slug, name, platform, publicKey)
}

func nameAndSlug(name string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("%w: name is required", ErrInvalid)
	}
	slug := Slugify(name)
	if slug == "" {
		return "", "", fmt.Errorf("%w: name has no slug-able characters", ErrInvalid)
	}
	return name, slug, nil
}

func (s *PGStore) insert(id, teamID, slug, name, platform, publicKey string) (Project, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	now := s.now().UTC()
	platform = strings.TrimSpace(platform)
	var team any
	if teamID != "" {
		team = teamID
	}
	if _, err := tx.Exec(
		`INSERT INTO goflare.projects (id, slug, name, platform, team_id, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		id, slug, name, platform, team, now,
	); err != nil {
		return Project{}, insertErr(err)
	}
	if _, err := tx.Exec(
		`INSERT INTO goflare.dsn_keys (public_key, project_id, label, created_at) VALUES ($1,$2,'default',$3)`,
		publicKey, id, now,
	); err != nil {
		return Project{}, insertErr(err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, err
	}
	return Project{
		ID: id, Slug: slug, Name: name, Platform: platform, TeamID: teamID,
		Keys:      []Key{{PublicKey: publicKey, Label: "default", CreatedAt: now}},
		CreatedAt: now,
	}, nil
}

func insertErr(err error) error {
	if isUnique(err) {
		return ErrExists
	}
	return err
}

func isUnique(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
