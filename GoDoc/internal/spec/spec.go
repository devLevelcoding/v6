// Package spec is the request body for each GoDoc format. A request names a
// format (csv | xml | pdf), carries that format's options, and either
// positional rows or keyed records / a raw JSON tree. Validation lives here;
// turning a spec into bytes is each renderer package's job.
package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Format is the output kind.
type Format string

const (
	FormatCSV Format = "csv"
	FormatXML Format = "xml"
	FormatPDF Format = "pdf"
)

// ErrInvalid is returned by Validate.
var ErrInvalid = errors.New("spec: invalid")

// Request is the unified body for POST /v1/render. The per-format endpoints
// (/v1/csv etc.) decode straight into the matching sub-spec.
type Request struct {
	Format   Format  `json:"format"`
	Filename string  `json:"filename,omitempty"` // Content-Disposition name; a default is derived
	CSV      *CSV    `json:"csv,omitempty"`
	XML      *XMLDoc `json:"xml,omitempty"`
	PDF      *PDF    `json:"pdf,omitempty"`
}

// Validate checks the request shape and the selected sub-spec.
func (r *Request) Validate() error {
	switch r.Format {
	case FormatCSV:
		if r.CSV == nil {
			return fmt.Errorf("%w: format is \"csv\" but the csv block is missing", ErrInvalid)
		}
		return r.CSV.Validate()
	case FormatXML:
		if r.XML == nil {
			return fmt.Errorf("%w: format is \"xml\" but the xml block is missing", ErrInvalid)
		}
		return r.XML.Validate()
	case FormatPDF:
		if r.PDF == nil {
			return fmt.Errorf("%w: format is \"pdf\" but the pdf block is missing", ErrInvalid)
		}
		return r.PDF.Validate()
	case "":
		return fmt.Errorf("%w: format is required (csv | xml | pdf)", ErrInvalid)
	default:
		return fmt.Errorf("%w: unknown format %q", ErrInvalid, r.Format)
	}
}

// --- CSV ---

// CSV is a tabular export. Provide Rows (positional) or Records (keyed, then
// projected through Columns). Columns is the header and, for Records, the
// projection order; when omitted it is the sorted union of the record keys.
type CSV struct {
	Columns   []string         `json:"columns,omitempty"`
	Rows      [][]any          `json:"rows,omitempty"`
	Records   []map[string]any `json:"records,omitempty"`
	Delimiter string           `json:"delimiter,omitempty"` // one rune; default ","
	NoHeader  bool             `json:"no_header,omitempty"`
	BOM       bool             `json:"bom,omitempty"` // prepend a UTF-8 BOM (Excel)
}

// Validate reports the first problem with the CSV spec.
func (c *CSV) Validate() error {
	if len(c.Rows) == 0 && len(c.Records) == 0 {
		return fmt.Errorf("%w: csv needs rows or records", ErrInvalid)
	}
	if len(c.Rows) > 0 && len(c.Records) > 0 {
		return fmt.Errorf("%w: csv has both rows and records", ErrInvalid)
	}
	if len(c.Records) > 0 && len(c.Columns) == 0 {
		// allowed — columns derived below, but every record must be an object
		for i, rec := range c.Records {
			if rec == nil {
				return fmt.Errorf("%w: csv.records[%d] is null", ErrInvalid, i)
			}
		}
	}
	if d := []rune(c.Delimiter); len(d) > 1 {
		return fmt.Errorf("%w: csv.delimiter must be a single character", ErrInvalid)
	}
	return nil
}

// --- XML ---

// XMLDoc is an XML export. With Template set, Data is passed to that named
// template; otherwise Data is converted generically (see internal/xmldoc):
// object → element per key, a key beginning "@" → attribute, "#text" → text,
// array → repeated element.
type XMLDoc struct {
	Template    string          `json:"template,omitempty"`
	Root        string          `json:"root,omitempty"` // generic mode: root element, default "root"
	Data        json.RawMessage `json:"data"`
	Declaration *bool           `json:"declaration,omitempty"` // emit <?xml ...?>; default true
	Indent      bool            `json:"indent,omitempty"`
}

// Validate reports the first problem with the XML spec.
func (x *XMLDoc) Validate() error {
	if len(x.Data) == 0 {
		return fmt.Errorf("%w: xml.data is required", ErrInvalid)
	}
	if !json.Valid(x.Data) {
		return fmt.Errorf("%w: xml.data is not valid JSON", ErrInvalid)
	}
	if x.Template == "" && x.Root != "" && !isName(x.Root) {
		return fmt.Errorf("%w: xml.root %q is not a valid element name", ErrInvalid, x.Root)
	}
	return nil
}

// WantDeclaration reports whether the <?xml?> prolog should be emitted.
func (x *XMLDoc) WantDeclaration() bool { return x.Declaration == nil || *x.Declaration }

// --- PDF ---

// PDF is a PDF export rendered from a named template ("table" or "invoice").
type PDF struct {
	Template string          `json:"template"`
	Title    string          `json:"title,omitempty"`
	Data     json.RawMessage `json:"data"`
	Page     Page            `json:"page,omitempty"`
}

// Page is the PDF page setup.
type Page struct {
	Size        string  `json:"size,omitempty"`        // "A4" (default) | "Letter" | "Legal"
	Orientation string  `json:"orientation,omitempty"` // "P" (default) | "L"
	MarginMM    float64 `json:"margin_mm,omitempty"`   // default 15
}

// Validate reports the first problem with the PDF spec.
func (p *PDF) Validate() error {
	switch strings.ToLower(p.Template) {
	case "table", "invoice":
	case "":
		return fmt.Errorf("%w: pdf.template is required (table | invoice)", ErrInvalid)
	default:
		return fmt.Errorf("%w: unknown pdf.template %q", ErrInvalid, p.Template)
	}
	if len(p.Data) == 0 || !json.Valid(p.Data) {
		return fmt.Errorf("%w: pdf.data must be valid JSON", ErrInvalid)
	}
	switch o := strings.ToUpper(p.Page.Orientation); o {
	case "", "P", "L":
	default:
		return fmt.Errorf("%w: pdf.page.orientation must be P or L", ErrInvalid)
	}
	return nil
}

// isName is a conservative XML element-name check.
func isName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || r == '-' || r == '.' || r == ':' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}
