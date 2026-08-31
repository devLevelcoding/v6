// Package pdfdoc renders a spec.PDF from a named template using go-pdf/fpdf.
// Two templates in Phase 0: "table" (a paginated report) and "invoice". The
// PDF is built in memory by fpdf and then written to w in one shot — fpdf has
// no streaming API, but a report is kilobytes and the work is off any Node
// event loop, which is the point.
package pdfdoc

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/levelcodingdev/godoc/internal/spec"
)

// Templates lists the named PDF formats.
func Templates() []string { return []string{"table", "invoice"} }

// Render writes the PDF for s to w.
func Render(w io.Writer, s *spec.PDF) error {
	orient := strings.ToUpper(s.Page.Orientation)
	if orient == "" {
		orient = "P"
	}
	size := s.Page.Size
	if size == "" {
		size = "A4"
	}
	margin := s.Page.MarginMM
	if margin <= 0 {
		margin = 15
	}

	pdf := fpdf.New(orient, "mm", size, "")
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(true, margin)
	pdf.SetTitle(fallback(s.Title, "Document"), true)
	pdf.SetCreator("GoDoc", false)

	var err error
	switch strings.ToLower(s.Template) {
	case "table":
		err = renderTable(pdf, s)
	case "invoice":
		err = renderInvoice(pdf, s)
	default:
		return fmt.Errorf("pdfdoc: unknown template %q", s.Template)
	}
	if err != nil {
		return err
	}
	if pdf.Err() {
		return fmt.Errorf("pdfdoc: %w", pdf.Error())
	}
	return pdf.Output(w)
}

func fallback(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("pdfdoc: data: %w", err)
	}
	return nil
}
