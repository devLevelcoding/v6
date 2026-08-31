package catalog

import (
	"errors"
	"testing"
)

func TestMemoryDatabaseLifecycle(t *testing.T) {
	c := NewMemory()
	if err := c.CreateDatabase("Analytics"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := c.CreateDatabase("analytics"); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if got := c.Databases(); len(got) != 1 || got[0] != "analytics" {
		t.Fatalf("databases = %v", got)
	}
}

func TestMemorySchemaAndTable(t *testing.T) {
	c := NewMemory()
	if err := c.CreateSchema("db", "public"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := c.CreateDatabase("db"); err != nil {
		t.Fatalf("create db: %v", err)
	}
	if err := c.CreateSchema("db", "public"); err != nil {
		t.Fatalf("schema: %v", err)
	}
	tbl := Table{Name: "leads", Columns: []Column{{Name: "id", Type: "int"}}}
	if err := c.CreateTable("db", "public", tbl); err != nil {
		t.Fatalf("table: %v", err)
	}
	got, err := c.Table("db", "public", "leads")
	if err != nil || got.Name != "leads" || len(got.Columns) != 1 {
		t.Fatalf("table = %+v err=%v", got, err)
	}
	if _, err := c.Table("db", "public", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if names, err := c.Tables("db", "public"); err != nil || len(names) != 1 || names[0] != "leads" {
		t.Fatalf("tables = %v err=%v", names, err)
	}
}
