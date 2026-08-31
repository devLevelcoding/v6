package query

import (
	"context"
	"testing"

	"github.com/levelcodingdev/gosnow/internal/catalog"
)

// CoverGo U1 — statement-path cost through the skeleton engine. These exist so
// the U10 DuckDB execution engine swap is measured against a real "before".

func benchEngine(b *testing.B) *Coordinator {
	b.Helper()
	cat := catalog.NewMemory()
	_ = cat.CreateDatabase("analytics")
	_ = cat.CreateSchema("analytics", "raw")
	return NewCoordinator(cat)
}

func BenchmarkExecuteSelectLiteral(b *testing.B) {
	e := benchEngine(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := e.Execute(ctx, Request{SQL: "SELECT 1"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecuteShow(b *testing.B) {
	e := benchEngine(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := e.Execute(ctx, Request{SQL: "SHOW DATABASES"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecuteUnsupported(b *testing.B) {
	e := benchEngine(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = e.Execute(ctx, Request{SQL: "SELECT * FROM analytics.raw.events WHERE ts > now()"})
	}
}
