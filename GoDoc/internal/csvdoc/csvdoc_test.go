package csvdoc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/levelcodingdev/godoc/internal/spec"
)

func render(t *testing.T, s *spec.CSV) string {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, s); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestRowsWithHeader(t *testing.T) {
	got := render(t, &spec.CSV{
		Columns: []string{"id", "name", "amount"},
		Rows:    [][]any{{1, "Ana", 12.5}, {2, "Bo, Jr", 3.0}},
	})
	want := "id,name,amount\n1,Ana,12.5\n2,\"Bo, Jr\",3\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRecordsDeriveColumns(t *testing.T) {
	got := render(t, &spec.CSV{
		Records: []map[string]any{{"b": 2, "a": 1}, {"a": 3, "b": 4}},
	})
	if !strings.HasPrefix(got, "a,b\n") { // derived columns are sorted
		t.Fatalf("header not sorted-derived: %q", got)
	}
	if !strings.Contains(got, "1,2\n") || !strings.Contains(got, "3,4\n") {
		t.Fatalf("rows wrong: %q", got)
	}
}

func TestRecordsProjectThroughColumns(t *testing.T) {
	got := render(t, &spec.CSV{
		Columns: []string{"name", "missing"},
		Records: []map[string]any{{"name": "x", "extra": "ignored"}},
	})
	if got != "name,missing\nx,\n" {
		t.Fatalf("projection wrong: %q", got)
	}
}

func TestDelimiterAndNoHeaderAndBOM(t *testing.T) {
	got := render(t, &spec.CSV{
		Rows:      [][]any{{1, 2}},
		Delimiter: ";",
		NoHeader:  true,
		BOM:       true,
	})
	if !strings.HasPrefix(got, "\xEF\xBB\xBF") {
		t.Fatal("missing UTF-8 BOM")
	}
	if !strings.Contains(got, "1;2") {
		t.Fatalf("delimiter not applied: %q", got)
	}
}

func TestCellFormatting(t *testing.T) {
	for in, want := range map[any]string{
		nil:           "",
		"s":           "s",
		true:          "true",
		float64(42):   "42",
		float64(4.25): "4.25",
	} {
		if got := cell(in); got != want {
			t.Errorf("cell(%v) = %q, want %q", in, got, want)
		}
	}
}
