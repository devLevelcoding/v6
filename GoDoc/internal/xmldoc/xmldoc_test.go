package xmldoc

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/levelcodingdev/godoc/internal/spec"
)

func render(t *testing.T, s *spec.XMLDoc) string {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, s); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	// every rendering must be well-formed XML
	if err := xml.Unmarshal([]byte(out), new(struct {
		XMLName xml.Name
	})); err != nil {
		t.Fatalf("output is not well-formed XML: %v\n%s", err, out)
	}
	return out
}

func p(s string) *bool { b := s == "true"; return &b }

func TestGenericConversion(t *testing.T) {
	out := render(t, &spec.XMLDoc{
		Root: "payroll",
		Data: json.RawMessage(`{
			"@period": "2026-08",
			"employees": [
				{"@id": "1", "name": "Ana", "net": 2500},
				{"@id": "2", "name": "Bo & Co", "note": {"#text": "raise <pending>"}}
			]
		}`),
	})
	if !strings.Contains(out, `<payroll period="2026-08">`) {
		t.Fatalf("attribute not on root: %s", out)
	}
	if strings.Count(out, "<employees ") != 2 {
		t.Fatalf("array should repeat the element twice: %s", out)
	}
	if !strings.Contains(out, `<name>Ana</name>`) || !strings.Contains(out, `<net>2500</net>`) {
		t.Fatalf("child elements wrong: %s", out)
	}
	if !strings.Contains(out, `Bo &amp; Co`) || !strings.Contains(out, `raise &lt;pending&gt;`) {
		t.Fatalf("escaping wrong: %s", out)
	}
}

func TestDeclarationToggleAndIndent(t *testing.T) {
	with := render(t, &spec.XMLDoc{Data: json.RawMessage(`{"a":1}`)})
	if !strings.HasPrefix(with, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatal("declaration should be on by default")
	}
	without := render(t, &spec.XMLDoc{Declaration: p("false"), Data: json.RawMessage(`{"a":1}`)})
	if strings.Contains(without, "<?xml") {
		t.Fatalf("declaration should be suppressed: %s", without)
	}
	indented := render(t, &spec.XMLDoc{Indent: true, Data: json.RawMessage(`{"a":{"b":1}}`)})
	if !strings.Contains(indented, "\n  <a>") {
		t.Fatalf("not indented: %q", indented)
	}
}

func TestEmptyAndScalar(t *testing.T) {
	out := render(t, &spec.XMLDoc{Root: "r", Data: json.RawMessage(`{"empty":null,"n":5}`)})
	if !strings.Contains(out, "<empty/>") || !strings.Contains(out, "<n>5</n>") {
		t.Fatalf("empty/scalar handling: %s", out)
	}
}

func TestPayrollTemplate(t *testing.T) {
	out := render(t, &spec.XMLDoc{
		Template: "payroll",
		Data: json.RawMessage(`{
			"period":"2026-08",
			"employer":{"name":"Acme SRL","tax_id":"RO123"},
			"employees":[
				{"id":"E1","name":"Ana","gross":3000,"tax":450,"deductions":50,"net":2500},
				{"id":"E2","name":"Bo","gross":2000,"tax":300,"deductions":0,"net":1700}
			]
		}`),
	})
	if !strings.Contains(out, `<payroll period="2026-08">`) ||
		!strings.Contains(out, `<employees count="2">`) ||
		!strings.Contains(out, `<totals><gross>5000.00</gross><net>4200.00</net></totals>`) {
		t.Fatalf("payroll output wrong:\n%s", out)
	}
}

func TestUnknownTemplate(t *testing.T) {
	var b bytes.Buffer
	err := Render(&b, &spec.XMLDoc{Template: "nope", Data: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected an error for an unknown template")
	}
}
