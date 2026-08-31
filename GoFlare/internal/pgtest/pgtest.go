// Package pgtest gives the Postgres-backed store tests a throwaway database.
// Set GOFLARE_TEST_DATABASE_URL to a scratch Postgres to run them; without it
// the tests skip. Each DB() call takes a session-level advisory lock (so test
// packages sharing one physical database serialize) then drops and recreates
// the goflare schema, giving every test a clean slate.
package pgtest

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/levelcodingdev/goflare/internal/pg"
)

// advisoryKey is an arbitrary constant every GoFlare pgtest run agrees on.
const advisoryKey = 0x60F1A2E

// DB returns a clean connection to the scratch database, or skips the test.
func DB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("GOFLARE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set GOFLARE_TEST_DATABASE_URL to run Postgres store tests")
	}
	db, err := pg.Open(url)
	if err != nil {
		t.Fatalf("pgtest: open %q: %v", url, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// A dedicated connection holds the advisory lock for the life of the test,
	// so concurrent test packages don't reset the schema under each other.
	lockConn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		t.Fatalf("pgtest: conn: %v", err)
	}
	if _, err := lockConn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryKey); err != nil {
		lockConn.Close()
		db.Close()
		t.Fatalf("pgtest: advisory lock: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS goflare CASCADE; CREATE SCHEMA goflare`); err != nil {
		_, _ = lockConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, advisoryKey)
		lockConn.Close()
		db.Close()
		t.Fatalf("pgtest: reset schema: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DROP SCHEMA IF EXISTS goflare CASCADE`)
		_, _ = lockConn.ExecContext(bg, `SELECT pg_advisory_unlock($1)`, advisoryKey)
		lockConn.Close()
		db.Close()
	})
	return db
}
