package pdfdoc

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/levelcodingdev/godoc/internal/spec"
)

func renderOK(t *testing.T, s *spec.PDF) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, s); err != nil {
		t.Fatal(err)
	}
	out := b.Bytes()
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("missing PDF header: %q", out[:min(8, len(out))])
	}
	if !bytes.Contains(out, []byte("EOF")) {
		t.Fatal("PDF trailer marker not found")
	}
	if len(out) < 400 {
		t.Fatalf("PDF suspiciously small: %d bytes", len(out))
	}
	return out
}

func TestTableTemplate(t *testing.T) {
	renderOK(t, &spec.PDF{
		Template: "table",
		Title:    "Q3 Report",
		Data: json.RawMessage(`{
			"columns": ["Region", "Units", "Revenue"],
			"rows": [["EU", 120, 45000.5], ["US", 300, 110000], ["APAC", 90, 30000]]
		}`),
	})
}

func TestTableManyRowsPaginate(t *testing.T) {
	rows := make([][]any, 200)
	for i := range rows {
		rows[i] = []any{i, "row " + string(rune('A'+i%26)), float64(i) * 1.5}
	}
	data, _ := json.Marshal(map[string]any{"columns": []string{"n", "label", "value"}, "rows": rows})
	out := renderOK(t, &spec.PDF{Template: "table", Title: "Big", Data: data})
	// "/Type /Page" is a substring of "/Type /Pages", so 1 page → 2 hits, N → N+1.
	if pages := bytes.Count(out, []byte("/Type /Page")) - 1; pages < 2 {
		t.Fatalf("200 rows produced %d page(s), want ≥ 2", pages)
	}
}

func TestInvoiceTemplate(t *testing.T) {
	out := renderOK(t, &spec.PDF{
		Template: "invoice",
		Data: json.RawMessage(`{
			"number": "INV-2026-014",
			"date": "2026-08-30",
			"currency": "EUR",
			"tax_rate": 19,
			"from": {"name": "Acme SRL", "address": "Str. Exemplu 1\nBucuresti", "email": "billing@acme.ro"},
			"to":   {"name": "Client GmbH", "address": "Berlin"},
			"items": [
				{"description": "Consulting", "quantity": 10, "unit_price": 120},
				{"description": "Hosting",    "quantity": 1,  "unit_price": 49.99}
			],
			"notes": "Payment within 14 days."
		}`),
	})
	// One page, valid PDF (renderOK checked the header/trailer/size). fpdf
	// compresses the content stream, so the visible text isn't greppable here —
	// a golden-image / text-extraction check is a later-phase test.
	if pages := bytes.Count(out, []byte("/Type /Page")) - 1; pages != 1 {
		t.Fatalf("invoice produced %d pages, want 1", pages)
	}
}

func TestUnknownTemplate(t *testing.T) {
	var b bytes.Buffer
	if err := Render(&b, &spec.PDF{Template: "poster", Data: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("expected an error for an unknown template")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
