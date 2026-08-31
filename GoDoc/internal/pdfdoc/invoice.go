package pdfdoc

import (
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/levelcodingdev/godoc/internal/spec"
)

type party struct {
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	Email   string `json:"email,omitempty"`
	TaxID   string `json:"tax_id,omitempty"`
}

type lineItem struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
}

func (li lineItem) amount() float64 { return li.Quantity * li.UnitPrice }

type invoiceData struct {
	Number   string     `json:"number"`
	Date     string     `json:"date,omitempty"`
	Due      string     `json:"due,omitempty"`
	Currency string     `json:"currency,omitempty"`
	From     party      `json:"from"`
	To       party      `json:"to"`
	Items    []lineItem `json:"items"`
	TaxRate  float64    `json:"tax_rate,omitempty"` // percent, e.g. 19
	Notes    string     `json:"notes,omitempty"`
}

func renderInvoice(pdf *fpdf.Fpdf, s *spec.PDF) error {
	var d invoiceData
	if err := decode(s.Data, &d); err != nil {
		return err
	}
	cur := d.Currency
	if cur == "" {
		cur = "EUR"
	}
	money := func(v float64) string { return fmt.Sprintf("%s %0.2f", cur, v) }

	pdf.AddPage()
	left, _, right, _ := pdf.GetMargins()
	pageW, _ := pdf.GetPageSize()
	usable := pageW - left - right

	pdf.SetFont("Helvetica", "B", 22)
	pdf.CellFormat(0, 12, fallback(s.Title, "INVOICE"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(90, 95, 100)
	pdf.CellFormat(0, 6, "No. "+d.Number, "", 1, "L", false, 0, "")
	if d.Date != "" {
		pdf.CellFormat(0, 6, "Date: "+d.Date, "", 1, "L", false, 0, "")
	}
	if d.Due != "" {
		pdf.CellFormat(0, 6, "Due: "+d.Due, "", 1, "L", false, 0, "")
	}
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	half := usable / 2
	y := pdf.GetY()
	partyBlock(pdf, left, y, half-4, "From", d.From)
	partyBlock(pdf, left+half, y, half-4, "Bill to", d.To)
	pdf.Ln(2)

	// items table
	wDesc, wQty, wUnit, wAmt := usable*0.52, usable*0.12, usable*0.18, usable*0.18
	pdf.SetFont("Helvetica", "B", 9.5)
	pdf.SetFillColor(238, 240, 244)
	pdf.CellFormat(wDesc, 7, "Description", "1", 0, "L", true, 0, "")
	pdf.CellFormat(wQty, 7, "Qty", "1", 0, "R", true, 0, "")
	pdf.CellFormat(wUnit, 7, "Unit", "1", 0, "R", true, 0, "")
	pdf.CellFormat(wAmt, 7, "Amount", "1", 1, "R", true, 0, "")

	pdf.SetFont("Helvetica", "", 9)
	var subtotal float64
	for _, it := range d.Items {
		subtotal += it.amount()
		pdf.CellFormat(wDesc, 6, it.Description, "LR", 0, "L", false, 0, "")
		pdf.CellFormat(wQty, 6, trimNum(it.Quantity), "LR", 0, "R", false, 0, "")
		pdf.CellFormat(wUnit, 6, money(it.UnitPrice), "LR", 0, "R", false, 0, "")
		pdf.CellFormat(wAmt, 6, money(it.amount()), "LR", 1, "R", false, 0, "")
	}
	pdf.CellFormat(usable, 0, "", "T", 1, "", false, 0, "")
	pdf.Ln(2)

	tax := subtotal * d.TaxRate / 100
	total := subtotal + tax
	totalsRow(pdf, usable, wDesc+wQty, "Subtotal", money(subtotal), false)
	if d.TaxRate > 0 {
		totalsRow(pdf, usable, wDesc+wQty, fmt.Sprintf("Tax (%s%%)", trimNum(d.TaxRate)), money(tax), false)
	}
	totalsRow(pdf, usable, wDesc+wQty, "Total", money(total), true)

	if strings.TrimSpace(d.Notes) != "" {
		pdf.Ln(6)
		pdf.SetFont("Helvetica", "I", 9)
		pdf.SetTextColor(90, 95, 100)
		pdf.MultiCell(usable, 5, d.Notes, "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}
	footer(pdf)
	return nil
}

func partyBlock(pdf *fpdf.Fpdf, x, y, w float64, label string, p party) {
	pdf.SetXY(x, y)
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetTextColor(140, 145, 150)
	pdf.CellFormat(w, 5, strings.ToUpper(label), "", 2, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(w, 5, p.Name, "", 2, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	for _, line := range []string{p.Address, p.Email, p.TaxID} {
		if line != "" {
			pdf.MultiCell(w, 4.5, line, "", "L", false)
			pdf.SetX(x)
		}
	}
}

func totalsRow(pdf *fpdf.Fpdf, usable, indent float64, label, value string, bold bool) {
	style := ""
	if bold {
		style = "B"
	}
	pdf.SetFont("Helvetica", style, 10)
	pdf.CellFormat(indent, 7, "", "", 0, "", false, 0, "")
	pdf.CellFormat(usable-indent-40, 7, label, "", 0, "R", false, 0, "")
	pdf.CellFormat(40, 7, value, "T", 1, "R", false, 0, "")
}

func trimNum(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}
