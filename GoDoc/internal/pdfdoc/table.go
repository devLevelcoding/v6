package pdfdoc

import (
	"strconv"

	"github.com/go-pdf/fpdf"

	"github.com/levelcodingdev/godoc/internal/spec"
)

// tableData is the input to the "table" template.
type tableData struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	// Widths are optional relative column weights; equal when omitted.
	Widths []float64 `json:"widths,omitempty"`
}

func renderTable(pdf *fpdf.Fpdf, s *spec.PDF) error {
	var d tableData
	if err := decode(s.Data, &d); err != nil {
		return err
	}
	pdf.AddPage()

	if s.Title != "" {
		pdf.SetFont("Helvetica", "B", 15)
		pdf.CellFormat(0, 9, s.Title, "", 1, "L", false, 0, "")
		pdf.Ln(3)
	}

	left, _, right, _ := pdf.GetMargins()
	pageW, _ := pdf.GetPageSize()
	usable := pageW - left - right
	widths := columnWidths(len(d.Columns), d.Widths, usable)

	drawHeader := func() {
		pdf.SetFont("Helvetica", "B", 9.5)
		pdf.SetFillColor(238, 240, 244)
		pdf.SetDrawColor(210, 214, 220)
		for i, c := range d.Columns {
			pdf.CellFormat(widths[i], 7, c, "1", 0, "L", true, 0, "")
		}
		pdf.Ln(-1)
	}
	drawHeader()

	pdf.SetFont("Helvetica", "", 9)
	pdf.SetDrawColor(224, 226, 230)
	for r, row := range d.Rows {
		if pdf.GetY()+6 > pageBottom(pdf) {
			pdf.AddPage()
			drawHeader()
			pdf.SetFont("Helvetica", "", 9)
		}
		fill := r%2 == 1
		pdf.SetFillColor(249, 250, 251)
		for i := range d.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			pdf.CellFormat(widths[i], 6, cell(v), "LR", 0, alignFor(v), fill, 0, "")
		}
		pdf.Ln(-1)
	}
	// close the table with a bottom rule
	pdf.CellFormat(sum(widths), 0, "", "T", 1, "", false, 0, "")

	footer(pdf)
	return nil
}

func columnWidths(n int, rel []float64, usable float64) []float64 {
	w := make([]float64, n)
	if len(rel) == n {
		var total float64
		for _, x := range rel {
			total += x
		}
		if total > 0 {
			for i, x := range rel {
				w[i] = usable * x / total
			}
			return w
		}
	}
	for i := range w {
		w[i] = usable / float64(n)
	}
	return w
}

func pageBottom(pdf *fpdf.Fpdf) float64 {
	_, h := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	return h - bottom
}

func footer(pdf *fpdf.Fpdf) {
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.SetTextColor(140, 145, 150)
		pdf.CellFormat(0, 8, "Page "+strconv.Itoa(pdf.PageNo()), "", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})
}

func alignFor(v any) string {
	if _, ok := v.(float64); ok {
		return "R"
	}
	return "L"
}

func sum(xs []float64) float64 {
	var t float64
	for _, x := range xs {
		t += x
	}
	return t
}

func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', 2, 64)
	default:
		return ""
	}
}
