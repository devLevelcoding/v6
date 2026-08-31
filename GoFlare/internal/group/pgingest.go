package group

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/levelcodingdev/goflare/internal/blob"
	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/uid"
)

// pgIngest is the Postgres equivalent of the in-memory Ingest: store the raw
// body in the blob store, then in one transaction upsert the issue for
// (project, fingerprint) and append the event to the index.
func (s *Store) pgIngest(projectID, hash string, e event.Event) (Issue, Outcome, error) {
	ctx := context.Background()

	if e.EventID == "" {
		e.EventID = uid.New()
	}
	body, err := json.Marshal(e)
	if err != nil {
		return Issue{}, "", err
	}
	key := blob.EventKey(projectID, e.EventID, e.Received)
	if err := s.blobs.Put(ctx, key, body); err != nil {
		return Issue{}, "", fmt.Errorf("group: blob put: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Issue{}, "", err
	}
	defer tx.Rollback() //nolint:errcheck

	issueID, outcome, err := upsertIssue(tx, projectID, hash, e)
	if err != nil {
		return Issue{}, "", err
	}

	if _, err := tx.Exec(`
		INSERT INTO goflare.events (event_id, issue_id, project_id, title, level, timestamp, received, blob_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (event_id) DO NOTHING`,
		e.EventID, issueID, projectID, e.Title(), string(e.Level), e.Timestamp, e.Received, key,
	); err != nil {
		return Issue{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Issue{}, "", err
	}

	iss, err := s.pgGet(issueID)
	if err != nil {
		return Issue{}, "", err
	}
	return iss, outcome, nil
}

// upsertIssue locks the (project, hash) issue row FOR UPDATE — so concurrent
// workers on the same fingerprint serialize — then inserts it or widens it,
// returning the issue id and what happened.
func upsertIssue(tx *sql.Tx, projectID, hash string, e event.Event) (string, Outcome, error) {
	var issueID, prevStatus string
	err := tx.QueryRow(
		`SELECT id, status FROM goflare.issues WHERE project_id = $1 AND hash = $2 FOR UPDATE`,
		projectID, hash).Scan(&issueID, &prevStatus)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		issueID = uid.New()[:16]
		if _, err := tx.Exec(`
			INSERT INTO goflare.issues
				(id, project_id, hash, title, culprit, level, platform, status, first_seen, last_seen, times_seen)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'unresolved',$8,$8,1)`,
			issueID, projectID, hash, e.Title(), e.Culprit(), string(e.Level), e.Platform, e.Timestamp); err != nil {
			return "", "", err
		}
		_, _ = tx.Exec(`INSERT INTO goflare.issue_status_history (issue_id, status) VALUES ($1,'unresolved')`, issueID)
		return issueID, OutcomeNew, nil

	case err != nil:
		return "", "", err
	}

	outcome := OutcomeRecurring
	newStatus, regressed := prevStatus, "regressed"
	if Status(prevStatus) == StatusResolved {
		newStatus, regressed, outcome = string(StatusUnresolved), "TRUE", OutcomeRegression
		_, _ = tx.Exec(`INSERT INTO goflare.issue_status_history (issue_id, status) VALUES ($1,$2)`, issueID, newStatus)
	}
	// times_seen++, widen the seen window, headline tracks the latest event,
	// level is the worst seen (rank compare done in SQL via a CASE ladder).
	if _, err := tx.Exec(`
		UPDATE goflare.issues SET
			times_seen = times_seen + 1,
			last_seen  = GREATEST(last_seen, $2),
			first_seen = LEAST(first_seen, $2),
			title      = $3,
			culprit    = CASE WHEN $4 <> '' THEN $4 ELSE culprit END,
			platform   = CASE WHEN $5 <> '' THEN $5 ELSE platform END,
			level      = CASE WHEN `+levelRankSQL("$6")+` > `+levelRankSQL("level")+` THEN $6 ELSE level END,
			status     = $7,
			regressed  = `+regressed+`
		WHERE id = $1`,
		issueID, e.Timestamp, e.Title(), e.Culprit(), e.Platform, string(e.Level), newStatus); err != nil {
		return "", "", err
	}
	return issueID, outcome, nil
}

// levelRankSQL renders the same severity ordering as levelRank(), for use in an
// UPDATE ... SET level = CASE comparison.
func levelRankSQL(expr string) string {
	return `CASE ` + expr + `
		WHEN 'fatal' THEN 5 WHEN 'error' THEN 4 WHEN 'warning' THEN 3
		WHEN 'info' THEN 2 WHEN 'debug' THEN 1 ELSE 0 END`
}

func (s *Store) pgEvents(issueID string, limit int) ([]event.Event, error) {
	if _, err := s.pgGet(issueID); err != nil {
		return nil, err
	}
	q := `SELECT blob_key FROM goflare.events WHERE issue_id = $1 ORDER BY received DESC`
	args := []any{issueID}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ctx := context.Background()
	var out []event.Event
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return out, err
		}
		body, err := s.blobs.Get(ctx, key)
		if err != nil {
			s.log.Warn("group: event body missing", "key", key, "err", err)
			continue
		}
		var e event.Event
		if err := json.Unmarshal(body, &e); err != nil {
			s.log.Warn("group: event body corrupt", "key", key, "err", err)
			continue
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PGEventCount is a Phase-1 helper for tests/metrics: total stored events for a
// project (the in-memory store only keeps a bounded sample, PG keeps them all).
func (s *Store) PGEventCount(projectID string) (int, error) {
	if s.db == nil {
		return 0, errors.New("group: not Postgres-backed")
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM goflare.events WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}
