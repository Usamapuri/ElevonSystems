package fiscal

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	"elevon-backend/internal/models"
)

// DIItem is one line of a PRAL Digital Invoicing payload (spec §7.4). The
// JSON tags are the wire contract with FBR — do not rename them.
type DIItem struct {
	HSCode                          string  `json:"hsCode"`
	ProductDescription              string  `json:"productDescription"`
	Rate                            string  `json:"rate"`
	UoM                             string  `json:"uoM"`
	Quantity                        float64 `json:"quantity"`
	TotalValues                     float64 `json:"totalValues"`
	ValueSalesExcludingST           float64 `json:"valueSalesExcludingST"`
	FixedNotifiedValueOrRetailPrice float64 `json:"fixedNotifiedValueOrRetailPrice"`
	SalesTaxApplicable              float64 `json:"salesTaxApplicable"`
	SalesTaxWithheldAtSource        float64 `json:"salesTaxWithheldAtSource"`
	ExtraTax                        string  `json:"extraTax"`
	FurtherTax                      float64 `json:"furtherTax"`
	SroScheduleNo                   string  `json:"sroScheduleNo"`
	FedPayable                      float64 `json:"fedPayable"`
	Discount                        float64 `json:"discount"`
	SaleType                        string  `json:"saleType"`
	SroItemSerialNo                 string  `json:"sroItemSerialNo"`
}

// DIInvoice is the top-level PRAL Digital Invoicing payload (spec §7.4).
// ScenarioID and Reason are omitempty: ScenarioID is sandbox-only and Reason
// only exists on a Debit Note.
type DIInvoice struct {
	InvoiceType string `json:"invoiceType"`
	InvoiceDate string `json:"invoiceDate"`

	SellerNTNCNIC      string `json:"sellerNTNCNIC"`
	SellerBusinessName string `json:"sellerBusinessName"`
	SellerProvince     string `json:"sellerProvince"`
	SellerAddress      string `json:"sellerAddress"`

	BuyerNTNCNIC          string `json:"buyerNTNCNIC"`
	BuyerBusinessName     string `json:"buyerBusinessName"`
	BuyerProvince         string `json:"buyerProvince"`
	BuyerAddress          string `json:"buyerAddress"`
	BuyerRegistrationType string `json:"buyerRegistrationType"`

	InvoiceRefNo string `json:"invoiceRefNo"`
	ScenarioID   string `json:"scenarioId,omitempty"`
	Reason       string `json:"reason,omitempty"`

	Items []DIItem `json:"items"`
}

// Buyer is the invoice's customer snapshot, taken from the customers row at
// sale time. A nil *Buyer on BuildInput means walk-in: the payload builder
// fills in the fixed walk-in fields itself (spec §7.4), no zero-value Buyer
// is needed by a caller.
type Buyer struct {
	NTNCNIC          string
	Name             string
	Province         string
	Address          string
	RegistrationType string
}

// BuildInput is everything BuildSaleInvoice/BuildDebitNote need beyond the
// fiscal Config. DefaultHSCode is settings.default_hs_code — it is passed in
// rather than read here so fiscal never imports settings (config.go's
// comment) and so no HS code literal ever lives in this package (CLAUDE.md
// invariant 7).
type BuildInput struct {
	Invoice       models.Invoice
	Buyer         *Buyer
	DefaultHSCode string
}

// ErrHSCodeMissing is returned when a line has no hs_code and
// DefaultHSCode is also blank. CLAUDE.md invariant 7: there is no HS-code
// default in code, so this is a refusal, never a fallback value.
var ErrHSCodeMissing = errors.New("fiscal_hs_code_missing")

// round2 matches the app's money rounding everywhere else: round half away
// from zero to 2 dp, on the float64 that already came out of a NUMERIC(12,2)
// column.
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// digitsOnly strips '-' and spaces from an NTN/CNIC, per the payload rule
// that buyerNTNCNIC is digits only.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isRegisteredBuyer reports whether a buyer is registered for the payload's
// purposes: RegistrationType must literally be "Registered" and the NTN/CNIC
// must have at least one digit once '-' and spaces are stripped.
func isRegisteredBuyer(b *Buyer) bool {
	if b == nil {
		return false
	}
	if b.RegistrationType != "Registered" {
		return false
	}
	return digitsOnly(b.NTNCNIC) != ""
}

// buildBuyerFields resolves the six buyer-related DIInvoice fields for both
// BuildSaleInvoice and BuildDebitNote, since a debit note carries the same
// buyer as the sale it reverses.
func buildBuyerFields(b *Buyer, cfg Config) (ntn, name, province, address, regType string) {
	if b == nil {
		return "", "Walk-in Customer", cfg.SellerProvince, "N/A", "Unregistered"
	}
	province = strings.TrimSpace(b.Province)
	if province == "" {
		province = cfg.SellerProvince
	}
	regType = b.RegistrationType
	if regType == "" {
		regType = "Unregistered"
	}
	return digitsOnly(b.NTNCNIC), b.Name, province, b.Address, regType
}

// scenarioFor resolves scenarioId: sandbox-only, SN001/SN002 (cfg's own
// values, not a literal) depending on registration.
func scenarioFor(registered bool, cfg Config) string {
	if !cfg.IsSandbox {
		return ""
	}
	if registered {
		return cfg.ScenarioRegistered
	}
	return cfg.ScenarioUnregistered
}

// buildItems turns invoice lines into DIItems (spec §7.4). further is the
// per-line further tax, already split by SplitFurtherTax and in the same
// order as inv.Lines.
func buildItems(inv models.Invoice, cfg Config, defaultHSCode string, further []float64) ([]DIItem, error) {
	items := make([]DIItem, len(inv.Lines))
	for i, line := range inv.Lines {
		hsCode := ""
		if line.HSCode != nil {
			hsCode = strings.TrimSpace(*line.HSCode)
		}
		if hsCode == "" {
			hsCode = strings.TrimSpace(defaultHSCode)
		}
		if hsCode == "" {
			return nil, ErrHSCodeMissing
		}

		uom := cfg.DefaultUoM
		if line.FBRUoM != nil && strings.TrimSpace(*line.FBRUoM) != "" {
			uom = *line.FBRUoM
		}

		valueExclST := round2(line.LineTotal - line.LineDiscount)
		salesTax := round2(line.LineTax)
		lineFurther := round2(further[i])
		total := round2(valueExclST + salesTax + lineFurther)

		items[i] = DIItem{
			HSCode:                          hsCode,
			ProductDescription:              line.ProductName + " " + strconv.FormatFloat(line.Quantity, 'f', 3, 64) + " kg",
			Rate:                            cfg.RateDesc,
			UoM:                             uom,
			Quantity:                        round3(line.Quantity),
			TotalValues:                     total,
			ValueSalesExcludingST:           valueExclST,
			FixedNotifiedValueOrRetailPrice: 0,
			SalesTaxApplicable:              salesTax,
			SalesTaxWithheldAtSource:        0,
			ExtraTax:                        "",
			FurtherTax:                      lineFurther,
			SroScheduleNo:                   "",
			FedPayable:                      0,
			Discount:                        round2(line.LineDiscount),
			SaleType:                        cfg.SaleType,
			SroItemSerialNo:                 "",
		}
	}
	return items, nil
}

// round3 formats quantity to 3 dp the same way the wire values do, so a
// quantity like 12.5000000001 from float arithmetic upstream never leaks
// through as extra noise.
func round3(x float64) float64 {
	return math.Round(x*1000) / 1000
}

// taxableValues returns each line's post-discount value, the base
// SplitFurtherTax divides further_tax_amount over.
func taxableValues(inv models.Invoice) []float64 {
	out := make([]float64, len(inv.Lines))
	for i, line := range inv.Lines {
		out[i] = round2(line.LineTotal - line.LineDiscount)
	}
	return out
}

// BuildSaleInvoice builds a Sale Invoice DI payload from an already-priced
// invoice (spec §7.4). It never recomputes tax — every money field on the
// wire is copied from invoice_lines, which pricing.ComputeTotals already
// priced server-side.
func BuildSaleInvoice(in BuildInput, cfg Config) (DIInvoice, error) {
	return build(in, cfg, "Sale Invoice", "", "")
}

// BuildDebitNote builds a Debit Note DI payload for a void (audit decisions
// 1-2): same lines as the original sale, invoiceRefNo the original FBR
// invoiceNumber, and a mandatory top-level reason. There is no Credit Note.
func BuildDebitNote(in BuildInput, cfg Config, refNumber, reason string) (DIInvoice, error) {
	return build(in, cfg, "Debit Note", refNumber, reason)
}

func build(in BuildInput, cfg Config, invoiceType, refNumber, reason string) (DIInvoice, error) {
	further := SplitFurtherTax(round2(in.Invoice.FurtherTaxAmount), taxableValues(in.Invoice))
	items, err := buildItems(in.Invoice, cfg, in.DefaultHSCode, further)
	if err != nil {
		return DIInvoice{}, err
	}

	registered := isRegisteredBuyer(in.Buyer)
	ntn, name, province, address, regType := buildBuyerFields(in.Buyer, cfg)

	return DIInvoice{
		InvoiceType:           invoiceType,
		InvoiceDate:           in.Invoice.BusinessDate.Format("2006-01-02"),
		SellerNTNCNIC:         cfg.SellerNTNCNIC,
		SellerBusinessName:    cfg.SellerBusinessName,
		SellerProvince:        cfg.SellerProvince,
		SellerAddress:         cfg.SellerAddress,
		BuyerNTNCNIC:          ntn,
		BuyerBusinessName:     name,
		BuyerProvince:         province,
		BuyerAddress:          address,
		BuyerRegistrationType: regType,
		InvoiceRefNo:          refNumber,
		ScenarioID:            scenarioFor(registered, cfg),
		Reason:                reason,
		Items:                 items,
	}, nil
}

// SplitFurtherTax divides total (further_tax_amount, already rounded to 2
// dp) across taxable's lines in proportion to their taxable value, working in
// integer paisa with the largest-remainder method so every line's share is
// off by at most one paisa from its exact proportional share. The last line
// always absorbs whatever residue is left after the other lines are fixed,
// so the returned slice sums to exactly total regardless of any rounding in
// between.
func SplitFurtherTax(total float64, taxable []float64) []float64 {
	n := len(taxable)
	result := make([]float64, n)
	if n == 0 {
		return result
	}
	if n == 1 {
		result[0] = round2(total)
		return result
	}

	totalPaisa := int64(math.Round(total * 100))

	var sumTaxable float64
	for _, t := range taxable {
		sumTaxable += t
	}

	type share struct {
		idx  int
		base int64
		frac float64
	}
	shares := make([]share, n)
	var distributed int64
	for i, t := range taxable {
		var raw float64
		if sumTaxable > 0 {
			raw = float64(totalPaisa) * t / sumTaxable
		}
		base := int64(math.Floor(raw))
		shares[i] = share{idx: i, base: base, frac: raw - float64(base)}
		distributed += base
	}
	remaining := totalPaisa - distributed

	sort.SliceStable(shares, func(a, b int) bool { return shares[a].frac > shares[b].frac })

	paisa := make([]int64, n)
	for i, s := range shares {
		paisa[s.idx] = s.base
		if int64(i) < remaining {
			paisa[s.idx]++
		}
	}

	// The last line absorbs whatever residue the split above leaves, so the
	// sum is exactly totalPaisa even if remaining were negative (total lower
	// than a naive floor sum, e.g. a negative total) or sumTaxable were 0.
	var sumOthers int64
	for i := 0; i < n-1; i++ {
		sumOthers += paisa[i]
	}
	paisa[n-1] = totalPaisa - sumOthers

	for i, p := range paisa {
		result[i] = float64(p) / 100
	}
	return result
}
