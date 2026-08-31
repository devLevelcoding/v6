package query

import (
	"context"
	"testing"

	"github.com/levelcodingdev/gosnow/internal/catalog"
)

// FuzzExecute (CoverGo U23) throws arbitrary statement text at the skeleton
// engine's parser. Contract: Execute never panics — it returns a Result or an
// error (ErrBadRequest / ErrUnsupported) for anything it can't run. It grows
// with the DuckDB pushdown work in U10.
func FuzzExecute(f *testing.F) {
	e := NewCoordinator(catalog.NewMemory())
	ctx := context.Background()

	for _, s := range []string{
		"SELECT 1", "SELECT 'hi';", "SELECT * FROM t",
		"CREATE DATABASE analytics", "CREATE SCHEMA analytics.raw",
		"SHOW DATABASES", "SHOW SCHEMAS IN analytics",
		"USE analytics", "", ";", "   ", "SeLeCt   1",
		"DROP TABLE x", "SELECT SELECT SELECT",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, sql string) {
		res, err := e.Execute(ctx, Request{SQL: sql})
		if err != nil {
			return
		}
		if res == nil {
			t.Fatalf("Execute(%q) returned nil Result and nil error", sql)
		}
		if len(res.Rows) != 0 && res.RowCount != len(res.Rows) {
			t.Fatalf("Execute(%q): RowCount=%d but len(Rows)=%d", sql, res.RowCount, len(res.Rows))
		}
	})
}
