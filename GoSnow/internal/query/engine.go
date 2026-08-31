// Package query is GoSnow's query coordinator. The walking-skeleton engine
// understands only a tiny slice of SQL (literal SELECT, CREATE
// DATABASE/SCHEMA, SHOW). Real execution — a vectorized columnar engine over
// Parquet in object storage — is future.md (Phase 2), most likely by embedding
// DuckDB or Apache DataFusion rather than writing one in Go.
package query

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/levelcodingdev/gosnow/internal/catalog"
)

var (
	// ErrBadRequest marks a malformed statement (caller error -> 400).
	ErrBadRequest = errors.New("query: malformed statement")
	// ErrUnsupported marks valid SQL the skeleton engine can't run yet (-> 422).
	ErrUnsupported = errors.New("query: unsupported statement")
)

// Request is one statement submitted for execution.
type Request struct {
	SQL       string
	Database  string
	Schema    string
	Warehouse string
}

// Result is a statement's outcome: a row set and/or a status message.
type Result struct {
	Columns  []string `json:"columns,omitempty"`
	Rows     [][]any  `json:"rows,omitempty"`
	RowCount int      `json:"rowCount"`
	Message  string   `json:"message,omitempty"`
}

// Engine executes SQL statements.
type Engine interface {
	Execute(ctx context.Context, req Request) (*Result, error)
}

// Coordinator is the skeleton Engine, backed by a catalog.
type Coordinator struct {
	cat catalog.Catalog
}

// NewCoordinator wires an engine to a catalog.
func NewCoordinator(cat catalog.Catalog) *Coordinator {
	return &Coordinator{cat: cat}
}

// Execute runs one statement.
func (c *Coordinator) Execute(_ context.Context, req Request) (*Result, error) {
	sql := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(req.SQL), ";"))
	if sql == "" {
		return nil, fmt.Errorf("%w: empty statement", ErrBadRequest)
	}
	fields := strings.Fields(sql)
	switch strings.ToUpper(fields[0]) {
	case "SELECT":
		return execSelect(sql)
	case "CREATE":
		return c.execCreate(fields, req)
	case "SHOW":
		return c.execShow(fields, req)
	case "USE":
		return &Result{Message: "USE accepted (session context is a no-op in the skeleton)"}, nil
	default:
		return nil, fmt.Errorf("%w: %s — see future.md (Phase 2: execution engine)", ErrUnsupported, strings.ToUpper(fields[0]))
	}
}

func execSelect(sql string) (*Result, error) {
	expr := strings.TrimSpace(sql[len("SELECT"):])
	quoted := len(expr) >= 2 && strings.HasPrefix(expr, "'") && strings.HasSuffix(expr, "'")
	if expr == "" || (!quoted && strings.ContainsAny(expr, " \t,()")) {
		return nil, fmt.Errorf("%w: only `SELECT <literal>` is supported — real SELECT is future.md Phase 2", ErrUnsupported)
	}
	var val any
	switch {
	case quoted:
		val = expr[1 : len(expr)-1]
	default:
		if n, err := strconv.ParseInt(expr, 10, 64); err == nil {
			val = n
		} else if f, err := strconv.ParseFloat(expr, 64); err == nil {
			val = f
		} else {
			return nil, fmt.Errorf("%w: cannot evaluate %q", ErrUnsupported, expr)
		}
	}
	return &Result{Columns: []string{expr}, Rows: [][]any{{val}}, RowCount: 1}, nil
}

func (c *Coordinator) execCreate(fields []string, req Request) (*Result, error) {
	if len(fields) < 3 {
		return nil, fmt.Errorf("%w: incomplete CREATE", ErrBadRequest)
	}
	switch strings.ToUpper(fields[1]) {
	case "DATABASE":
		name := catalog.Ident(fields[2])
		if err := c.cat.CreateDatabase(name); err != nil {
			return nil, err
		}
		return &Result{Message: "database " + name + " created"}, nil
	case "SCHEMA":
		db, name, err := schemaRef(fields[2], req.Database)
		if err != nil {
			return nil, err
		}
		if err := c.cat.CreateSchema(db, name); err != nil {
			return nil, err
		}
		return &Result{Message: "schema " + db + "." + name + " created"}, nil
	default:
		return nil, fmt.Errorf("%w: CREATE %s — see future.md", ErrUnsupported, strings.ToUpper(fields[1]))
	}
}

func (c *Coordinator) execShow(fields []string, req Request) (*Result, error) {
	if len(fields) < 2 {
		return nil, fmt.Errorf("%w: incomplete SHOW", ErrBadRequest)
	}
	switch strings.ToUpper(fields[1]) {
	case "DATABASES":
		return rows1("name", c.cat.Databases()), nil
	case "SCHEMAS":
		db := req.Database
		if len(fields) >= 4 && strings.ToUpper(fields[2]) == "IN" {
			db = fields[3]
		}
		if db == "" {
			return nil, fmt.Errorf("%w: SHOW SCHEMAS needs `IN <db>` or a session database", ErrBadRequest)
		}
		names, err := c.cat.Schemas(db)
		if err != nil {
			return nil, err
		}
		return rows1("name", names), nil
	default:
		return nil, fmt.Errorf("%w: SHOW %s", ErrUnsupported, strings.ToUpper(fields[1]))
	}
}

// schemaRef resolves "db.schema" or a bare "schema" against a session database.
func schemaRef(ref, sessionDB string) (db, name string, err error) {
	if i := strings.IndexByte(ref, '.'); i >= 0 {
		return catalog.Ident(ref[:i]), catalog.Ident(ref[i+1:]), nil
	}
	if sessionDB == "" {
		return "", "", fmt.Errorf("%w: schema %q has no database — use `db.schema` or set a session database", ErrBadRequest, ref)
	}
	return catalog.Ident(sessionDB), catalog.Ident(ref), nil
}

func rows1(col string, vals []string) *Result {
	rows := make([][]any, len(vals))
	for i, v := range vals {
		rows[i] = []any{v}
	}
	return &Result{Columns: []string{col}, Rows: rows, RowCount: len(rows)}
}
