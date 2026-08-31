// Package render dispatches a validated spec.Request to the right renderer and
// reports the HTTP content type and a default filename. Every renderer writes
// straight to the io.Writer it is given.
package render

import (
	"io"
	"time"

	"github.com/levelcodingdev/godoc/internal/csvdoc"
	"github.com/levelcodingdev/godoc/internal/pdfdoc"
	"github.com/levelcodingdev/godoc/internal/spec"
	"github.com/levelcodingdev/godoc/internal/xmldoc"
)

// Output describes what a render produces.
type Output struct {
	ContentType string
	Filename    string
}

// Do renders r to w. r must already have passed r.Validate().
func Do(w io.Writer, r *spec.Request) (Output, error) {
	switch r.Format {
	case spec.FormatCSV:
		return Output{"text/csv; charset=utf-8", name(r, "csv")}, csvdoc.Render(w, r.CSV)
	case spec.FormatXML:
		return Output{"application/xml; charset=utf-8", name(r, "xml")}, xmldoc.Render(w, r.XML)
	case spec.FormatPDF:
		return Output{"application/pdf", name(r, "pdf")}, pdfdoc.Render(w, r.PDF)
	default:
		return Output{}, spec.ErrInvalid
	}
}

// Templates is the set of named formats, by output kind.
func Templates() map[string][]string {
	return map[string][]string{
		"xml": xmldoc.Templates(),
		"pdf": pdfdoc.Templates(),
	}
}

func name(r *spec.Request, ext string) string {
	if r.Filename != "" {
		return r.Filename
	}
	base := string(r.Format)
	if r.Format == spec.FormatPDF && r.PDF != nil && r.PDF.Template != "" {
		base = r.PDF.Template
	}
	if r.Format == spec.FormatXML && r.XML != nil && r.XML.Template != "" {
		base = r.XML.Template
	}
	return base + "-" + time.Now().UTC().Format("20060102-150405") + "." + ext
}
