// Package csvdoc streams a spec.CSV to an io.Writer using encoding/csv — no
// buffering of the whole document, so a million-row export costs a constant
// amount of memory.
package csvdoc

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/levelcodingdev/godoc/internal/spec"
)

// Render writes the CSV for s to w.
func Render(w io.Writer, s *spec.CSV) error {
	if s.BOM {
		if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return err
		}
	}
	cw := csv.NewWriter(w)
	if d := []rune(s.Delimiter); len(d) == 1 {
		cw.Comma = d[0]
	}

	cols := s.Columns
	if len(s.Records) > 0 && len(cols) == 0 {
		cols = deriveColumns(s.Records)
	}

	if !s.NoHeader && len(cols) > 0 {
		if err := cw.Write(cols); err != nil {
			return err
		}
	}

	switch {
	case len(s.Rows) > 0:
		for i, row := range s.Rows {
			rec := make([]string, len(row))
			for j, v := range row {
				rec[j] = cell(v)
			}
			if err := cw.Write(rec); err != nil {
				return fmt.Errorf("csvdoc: row %d: %w", i, err)
			}
		}
	default:
		for i, r := range s.Records {
			rec := make([]string, len(cols))
			for j, c := range cols {
				rec[j] = cell(r[c])
			}
			if err := cw.Write(rec); err != nil {
				return fmt.Errorf("csvdoc: record %d: %w", i, err)
			}
		}
	}

	cw.Flush()
	return cw.Error()
}

func deriveColumns(records []map[string]any) []string {
	seen := map[string]struct{}{}
	for _, r := range records {
		for k := range r {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cell renders a JSON-decoded value as a CSV field.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// JSON numbers decode to float64; keep integers integral.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}
