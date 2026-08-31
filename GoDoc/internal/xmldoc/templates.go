package xmldoc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levelcodingdev/godoc/internal/spec"
)

var escaper = escapeText

var replacer = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;",
)
var attrReplacer = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
)

// Templates lists the named XML formats.
func Templates() []string { return []string{"payroll"} }

func renderTemplate(bw *bufio.Writer, s *spec.XMLDoc) error {
	switch strings.ToLower(s.Template) {
	case "payroll":
		return renderPayroll(bw, s)
	default:
		return fmt.Errorf("xmldoc: unknown template %q", s.Template)
	}
}

// payrollData is the input to the "payroll" template — the shape PayrollEngine
// builds today with xmlbuilder2.
type payrollData struct {
	Period   string `json:"period"` // e.g. "2026-08"
	Employer struct {
		Name  string `json:"name"`
		TaxID string `json:"tax_id"`
	} `json:"employer"`
	Employees []struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		Gross      float64 `json:"gross"`
		Tax        float64 `json:"tax"`
		Deductions float64 `json:"deductions"`
		Net        float64 `json:"net"`
	} `json:"employees"`
}

func renderPayroll(bw *bufio.Writer, s *spec.XMLDoc) error {
	var d payrollData
	if err := json.Unmarshal(s.Data, &d); err != nil {
		return fmt.Errorf("xmldoc: payroll data: %w", err)
	}
	nl := ""
	if s.Indent {
		nl = "\n"
	}
	p := func(depth int, format string, a ...any) {
		if s.Indent {
			bw.WriteString(strings.Repeat("  ", depth))
		}
		fmt.Fprintf(bw, format, a...)
		bw.WriteString(nl)
	}

	var totalGross, totalNet float64
	p(0, `<payroll period=%q>`, escaper(d.Period))
	p(1, `<employer><name>%s</name><taxId>%s</taxId></employer>`, escaper(d.Employer.Name), escaper(d.Employer.TaxID))
	p(1, `<employees count="%d">`, len(d.Employees))
	for _, e := range d.Employees {
		totalGross += e.Gross
		totalNet += e.Net
		p(2, `<employee id=%q>`, escaper(e.ID))
		p(3, `<name>%s</name>`, escaper(e.Name))
		p(3, `<gross>%.2f</gross><tax>%.2f</tax><deductions>%.2f</deductions><net>%.2f</net>`,
			e.Gross, e.Tax, e.Deductions, e.Net)
		p(2, `</employee>`)
	}
	p(1, `</employees>`)
	p(1, `<totals><gross>%.2f</gross><net>%.2f</net></totals>`, totalGross, totalNet)
	p(0, `</payroll>`)
	return nil
}
