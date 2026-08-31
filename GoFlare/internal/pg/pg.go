// Package pg opens the Postgres connection GoFlare's durable stores share.
// Everything lives in the `goflare` schema; each store package owns its own
// CREATE TABLE migration (project.Migrate, group.Migrate, org.Migrate) and runs
// it from UsePostgres — the same per-package pattern as GoAdmin's gobase.
package pg

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// Open connects to url (a libpq DSN or postgres:// URL), verifies the
// connection, applies conservative pool limits and ensures the goflare schema
// exists.
func Open(url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("pg: open: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pg: ping: %w", err)
	}
	// CREATE SCHEMA IF NOT EXISTS still races two concurrent creators to a
	// unique-violation on pg_namespace (a long-standing Postgres quirk); the
	// duplicate is exactly the outcome we wanted, so tolerate it.
	if _, err := db.Exec(`CREATE SCHEMA IF NOT EXISTS goflare`); err != nil && !isDuplicate(err) {
		db.Close()
		return nil, fmt.Errorf("pg: create schema: %w", err)
	}
	return db, nil
}

func isDuplicate(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && (pqErr.Code == "23505" || pqErr.Code == "42P06")
}
