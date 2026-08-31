// Package catalog is GoSnow's metadata layer: the namespace tree of
// databases -> schemas -> tables. Table *data* lives in package storage; this
// package only tracks structure. The in-memory implementation here is a
// stand-in for the Postgres-backed catalog planned in future.md (Phase 1).
package catalog

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrExists is returned when creating an object that already exists.
	ErrExists = errors.New("catalog: object already exists")
	// ErrNotFound is returned when a referenced object is missing.
	ErrNotFound = errors.New("catalog: object not found")
)

// Column is a single column definition.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Table is table metadata (not its rows).
type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

// Catalog is the metadata contract used by the query engine and API.
type Catalog interface {
	CreateDatabase(name string) error
	CreateSchema(db, schema string) error
	CreateTable(db, schema string, t Table) error
	Table(db, schema, name string) (Table, error)
	Databases() []string
	Schemas(db string) ([]string, error)
	Tables(db, schema string) ([]string, error)
}

// Ident normalizes an identifier: unquoted names fold to lower case,
// double-quoted names keep their exact spelling.
func Ident(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return strings.ToLower(s)
}

type schema struct {
	tables map[string]Table
}

type database struct {
	schemas map[string]*schema
}

// Memory is an in-memory Catalog, safe for concurrent use.
type Memory struct {
	mu  sync.RWMutex
	dbs map[string]*database
}

// NewMemory returns an empty in-memory catalog.
func NewMemory() *Memory {
	return &Memory{dbs: map[string]*database{}}
}

// CreateDatabase adds a new, empty database.
func (m *Memory) CreateDatabase(name string) error {
	name = Ident(name)
	if name == "" {
		return errors.New("catalog: empty database name")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.dbs[name]; ok {
		return ErrExists
	}
	m.dbs[name] = &database{schemas: map[string]*schema{}}
	return nil
}

// CreateSchema adds a schema to an existing database.
func (m *Memory) CreateSchema(db, name string) error {
	db, name = Ident(db), Ident(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.dbs[db]
	if !ok {
		return ErrNotFound
	}
	if _, ok := d.schemas[name]; ok {
		return ErrExists
	}
	d.schemas[name] = &schema{tables: map[string]Table{}}
	return nil
}

// CreateTable registers table metadata in an existing schema.
func (m *Memory) CreateTable(db, sch string, t Table) error {
	db, sch = Ident(db), Ident(sch)
	t.Name = Ident(t.Name)
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.schemaLocked(db, sch)
	if err != nil {
		return err
	}
	if _, ok := s.tables[t.Name]; ok {
		return ErrExists
	}
	s.tables[t.Name] = t
	return nil
}

// Table returns one table's metadata.
func (m *Memory) Table(db, sch, name string) (Table, error) {
	db, sch, name = Ident(db), Ident(sch), Ident(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.schemaLocked(db, sch)
	if err != nil {
		return Table{}, err
	}
	t, ok := s.tables[name]
	if !ok {
		return Table{}, ErrNotFound
	}
	return t, nil
}

// Databases lists database names, sorted.
func (m *Memory) Databases() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return sortedKeys(m.dbs)
}

// Schemas lists a database's schema names, sorted.
func (m *Memory) Schemas(db string) ([]string, error) {
	db = Ident(db)
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.dbs[db]
	if !ok {
		return nil, ErrNotFound
	}
	return sortedKeys(d.schemas), nil
}

// Tables lists a schema's table names, sorted.
func (m *Memory) Tables(db, sch string) ([]string, error) {
	db, sch = Ident(db), Ident(sch)
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.schemaLocked(db, sch)
	if err != nil {
		return nil, err
	}
	return sortedKeys(s.tables), nil
}

func (m *Memory) schemaLocked(db, sch string) (*schema, error) {
	d, ok := m.dbs[db]
	if !ok {
		return nil, ErrNotFound
	}
	s, ok := d.schemas[sch]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
