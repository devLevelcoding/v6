package spec

import (
	"encoding/json"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     Request
		wantErr bool
	}{
		{"csv rows", Request{Format: FormatCSV, CSV: &CSV{Rows: [][]any{{1, 2}}}}, false},
		{"csv records", Request{Format: FormatCSV, CSV: &CSV{Records: []map[string]any{{"a": 1}}}}, false},
		{"csv empty", Request{Format: FormatCSV, CSV: &CSV{}}, true},
		{"csv both", Request{Format: FormatCSV, CSV: &CSV{Rows: [][]any{{1}}, Records: []map[string]any{{"a": 1}}}}, true},
		{"csv 2-char delim", Request{Format: FormatCSV, CSV: &CSV{Rows: [][]any{{1}}, Delimiter: ";;"}}, true},
		{"csv missing block", Request{Format: FormatCSV}, true},

		{"xml generic", Request{Format: FormatXML, XML: &XMLDoc{Data: raw(`{"a":1}`)}}, false},
		{"xml template", Request{Format: FormatXML, XML: &XMLDoc{Template: "payroll", Data: raw(`{}`)}}, false},
		{"xml no data", Request{Format: FormatXML, XML: &XMLDoc{}}, true},
		{"xml bad json", Request{Format: FormatXML, XML: &XMLDoc{Data: raw(`{`)}}, true},
		{"xml bad root", Request{Format: FormatXML, XML: &XMLDoc{Root: "1 bad", Data: raw(`{}`)}}, true},

		{"pdf table", Request{Format: FormatPDF, PDF: &PDF{Template: "table", Data: raw(`{}`)}}, false},
		{"pdf invoice", Request{Format: FormatPDF, PDF: &PDF{Template: "invoice", Data: raw(`{}`)}}, false},
		{"pdf no template", Request{Format: FormatPDF, PDF: &PDF{Data: raw(`{}`)}}, true},
		{"pdf unknown template", Request{Format: FormatPDF, PDF: &PDF{Template: "poster", Data: raw(`{}`)}}, true},
		{"pdf bad orientation", Request{Format: FormatPDF, PDF: &PDF{Template: "table", Data: raw(`{}`), Page: Page{Orientation: "sideways"}}}, true},

		{"no format", Request{}, true},
		{"unknown format", Request{Format: "docx"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.req.Validate(); (err != nil) != c.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestWantDeclaration(t *testing.T) {
	no := false
	if !(&XMLDoc{}).WantDeclaration() {
		t.Fatal("default should be true")
	}
	if (&XMLDoc{Declaration: &no}).WantDeclaration() {
		t.Fatal("explicit false should stick")
	}
}
