package catalog

import (
	"strconv"
	"testing"
)

// CoverGo U1 — catalog read cost. The in-memory Memory catalog is the "before"
// for the Postgres-backed, snapshot-versioned catalog in GoSnow2 / U10.

func loaded(dbs, schemasPer, tablesPer int) *Memory {
	m := NewMemory()
	for d := 0; d < dbs; d++ {
		db := "db" + strconv.Itoa(d)
		_ = m.CreateDatabase(db)
		for s := 0; s < schemasPer; s++ {
			sch := "s" + strconv.Itoa(s)
			_ = m.CreateSchema(db, sch)
			for tbl := 0; tbl < tablesPer; tbl++ {
				_ = m.CreateTable(db, sch, Table{Name: "t" + strconv.Itoa(tbl)})
			}
		}
	}
	return m
}

func BenchmarkTableLookup(b *testing.B) {
	m := loaded(10, 10, 50)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Table("db5", "s5", "t25"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTablesList(b *testing.B) {
	m := loaded(10, 10, 50)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Tables("db5", "s5"); err != nil {
			b.Fatal(err)
		}
	}
}
