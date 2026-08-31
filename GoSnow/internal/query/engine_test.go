package query

import (
	"context"
	"errors"
	"testing"

	"github.com/levelcodingdev/gosnow/internal/catalog"
)

func TestExecuteLiterals(t *testing.T) {
	e := NewCoordinator(catalog.NewMemory())
	ctx := context.Background()

	r, err := e.Execute(ctx, Request{SQL: "SELECT 1"})
	if err != nil || r.RowCount != 1 || r.Rows[0][0].(int64) != 1 {
		t.Fatalf("SELECT 1 -> %+v err=%v", r, err)
	}
	r, err = e.Execute(ctx, Request{SQL: "SELECT 'hi';"})
	if err != nil || r.Rows[0][0].(string) != "hi" {
		t.Fatalf("SELECT 'hi' -> %+v err=%v", r, err)
	}
	if _, err := e.Execute(ctx, Request{SQL: "SELECT * FROM t"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestExecuteDDL(t *testing.T) {
	cat := catalog.NewMemory()
	e := NewCoordinator(cat)
	ctx := context.Background()

	if _, err := e.Execute(ctx, Request{SQL: "CREATE DATABASE analytics"}); err != nil {
		t.Fatalf("create db: %v", err)
	}
	if _, err := e.Execute(ctx, Request{SQL: "CREATE SCHEMA analytics.raw"}); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	r, err := e.Execute(ctx, Request{SQL: "SHOW DATABASES"})
	if err != nil || r.RowCount != 1 || r.Rows[0][0].(string) != "analytics" {
		t.Fatalf("show databases -> %+v err=%v", r, err)
	}
	r, err = e.Execute(ctx, Request{SQL: "SHOW SCHEMAS IN analytics"})
	if err != nil || r.RowCount != 1 || r.Rows[0][0].(string) != "raw" {
		t.Fatalf("show schemas -> %+v err=%v", r, err)
	}
	if _, err := e.Execute(ctx, Request{SQL: "DELETE FROM x"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}
