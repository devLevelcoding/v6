package group

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/levelcodingdev/goflare/internal/blob"
	"github.com/levelcodingdev/goflare/internal/event"
)

// UsePostgres switches the store to Postgres for issues + an event index and a
// blob.Store for raw event bodies. It runs the migration. After this call the
// in-memory maps and the snapshot hooks are dormant; wire the store before any
// ingest traffic. A nil logger falls back to slog.Default.
func (s *Store) UsePostgres(db *sql.DB, blobs blob.Store, log *slog.Logger) error {
	if blobs == nil {
		return errors.New("group: UsePostgres needs a blob store for event bodies")
	}
	if log == nil {
		log = slog.Default()
	}
	if err := migrateGroup(db); err != nil {
		return err
	}
	s.mu.Lock()
	s.db, s.blobs, s.log = db, blobs, log
	s.mu.Unlock()
	return nil
}

func migrateGroup(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS goflare.issues (
			id          TEXT PRIMARY KEY,
			project_id  TEXT NOT NULL,
			hash        TEXT NOT NULL,
			title       TEXT NOT NULL DEFAULT '',
			culprit     TEXT NOT NULL DEFAULT '',
			level       TEXT NOT NULL DEFAULT '',
			platform    TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT 'unresolved',
			regressed   BOOLEAN NOT NULL DEFAULT FALSE,
			first_seen  TIMESTAMPTZ NOT NULL,
			last_seen   TIMESTAMPTZ NOT NULL,
			times_seen  BIGINT NOT NULL DEFAULT 0,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (project_id, hash)
		);
		CREATE INDEX IF NOT EXISTS issues_project_lastseen_idx
			ON goflare.issues (project_id, last_seen DESC);
		CREATE INDEX IF NOT EXISTS issues_status_idx ON goflare.issues (status);

		CREATE TABLE IF NOT EXISTS goflare.issue_status_history (
			id       BIGSERIAL PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES goflare.issues(id) ON DELETE CASCADE,
			status   TEXT NOT NULL,
			at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS issue_status_history_issue_idx
			ON goflare.issue_status_history (issue_id, at DESC);

		CREATE TABLE IF NOT EXISTS goflare.events (
			event_id   TEXT PRIMARY KEY,
			issue_id   TEXT NOT NULL REFERENCES goflare.issues(id) ON DELETE CASCADE,
			project_id TEXT NOT NULL,
			title      TEXT NOT NULL DEFAULT '',
			level      TEXT NOT NULL DEFAULT '',
			timestamp  TIMESTAMPTZ NOT NULL,
			received   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			blob_key   TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS events_issue_received_idx
			ON goflare.events (issue_id, received DESC);
	`)
	if err != nil {
		return fmt.Errorf("group: migrate: %w", err)
	}
	return nil
}

const issueCols = `id, project_id, hash, title, culprit, level, platform, status, regressed, first_seen, last_seen, times_seen`

func scanIssue(row interface{ Scan(...any) error }) (Issue, error) {
	var i Issue
	var lvl, status string
	err := row.Scan(&i.ID, &i.ProjectID, &i.Hash, &i.Title, &i.Culprit, &lvl, &i.Platform,
		&status, &i.Regressed, &i.FirstSeen, &i.LastSeen, &i.TimesSeen)
	if err != nil {
		return Issue{}, err
	}
	i.Level = event.Level(lvl)
	i.Status = Status(status)
	return i, nil
}

func (s *Store) pgGet(id string) (Issue, error) {
	row := s.db.QueryRow(`SELECT `+issueCols+` FROM goflare.issues WHERE id = $1`, id)
	i, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return i, err
}

func (s *Store) pgList(f Filter) []Issue {
	q := `SELECT ` + issueCols + ` FROM goflare.issues WHERE 1=1`
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		q += fmt.Sprintf(clause, len(args))
	}
	if f.ProjectID != "" {
		add(" AND project_id = $%d", f.ProjectID)
	}
	if f.Status != "" {
		add(" AND status = $%d", string(f.Status))
	}
	if f.Query != "" {
		add(" AND (title ILIKE '%%'||$%d||'%%' OR culprit ILIKE '%%'||$%[1]d||'%%')", f.Query)
	}
	q += " ORDER BY last_seen DESC, id DESC LIMIT 500"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		s.log.Error("group: pgList", "err", err)
		return nil
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			s.log.Error("group: pgList scan", "err", err)
			return out
		}
		out = append(out, i)
	}
	return out
}

func (s *Store) pgSetStatus(id string, status Status) (Issue, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Issue{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	regressed := "regressed"
	if status == StatusUnresolved {
		regressed = "FALSE" // acknowledging clears the regression flag
	}
	res, err := tx.Exec(
		`UPDATE goflare.issues SET status = $1, regressed = `+regressed+` WHERE id = $2`,
		string(status), id)
	if err != nil {
		return Issue{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Issue{}, ErrNotFound
	}
	if _, err := tx.Exec(
		`INSERT INTO goflare.issue_status_history (issue_id, status) VALUES ($1,$2)`,
		id, string(status)); err != nil {
		return Issue{}, err
	}
	if err := tx.Commit(); err != nil {
		return Issue{}, err
	}
	return s.pgGet(id)
}
