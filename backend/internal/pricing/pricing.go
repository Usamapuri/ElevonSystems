// Package pricing is the one money function (spec §6.3): every invoice total
// is computed server-side from products.rate and the submitted quantities,
// never trusted from the client. All arithmetic after the first per-line
// rounding runs in int64 paisa so nothing drifts the way repeated float64
// addition can (port of the retail POS's moneyPaisa idiom, backend/internal/
// handlers/payments.go). lib/pricing.ts is the TS mirror; both suites load
// the same JSON fixture (testdata/pricing_fixture.json / frontend/src/lib/
// pricing_fixture.json) and a parity test in each pins the two files byte
// identical.
//
// Money-changing edits here are customer-visible (CLAUDE.md invariant 8):
// compute before/after examples and get the owner's sign-off before merge.
package pricing

import (
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"
)

// Errors ComputeTotals returns. Their Error() text is the stable snake_case
// API error code (never a client sees err.Error() directly here, but the
// caller maps these 1:1 onto the APIResponse error field).
var (
	ErrNoLines         = errors.New("no_lines")
	ErrInvalidQuantity = errors.New("invalid_quantity")
	ErrInvalidRate     = errors.New("invalid_rate")
)

// LineIn is one cart line as submitted: quantity in kg (3 dp from the
// weight pad; up to 4 dp accepted — see validQuantity), rate per kg (2 dp,
// always products.rate, never client money).
type LineIn struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  float64   `json:"quantity"`
	Rate      float64   `json:"rate"`
}

// Input is everything ComputeTotals needs. DiscountPercent nil means the
// invoice discount was entered as a rupee amount (DiscountAmount); non-nil
// means it was entered as a percentage of the subtotal (5 for 5%, not
// 0.05) and DiscountAmount is ignored. TaxRate and FurtherTaxRate are
// fractions (0.18, not 18) — callers resolve them with TaxRateFor first.
type Input struct {
	Lines           []LineIn `json:"lines"`
	DiscountAmount  float64  `json:"discount_amount"`
	DiscountPercent *float64 `json:"discount_percent"`
	TaxRate         float64  `json:"tax_rate"`
	FurtherTaxRate  float64  `json:"further_tax_rate"`
	BuyerRegistered bool     `json:"buyer_registered"`
}

// LineOut echoes the line's quantity/rate alongside its computed shares, so
// a receipt or the FBR payload (§7.4) never needs the original Input again.
type LineOut struct {
	Quantity     float64 `json:"quantity"`
	UnitPrice    float64 `json:"unit_price"`
	LineTotal    float64 `json:"line_total"`
	LineDiscount float64 `json:"line_discount"`
	LineTaxable  float64 `json:"line_taxable"`
	LineTax      float64 `json:"line_tax"`
}

// Totals is the full result. TotalPayable is a whole-rupee int64 (the
// figure the customer actually pays and the ledger carries, per spec D9);
// every other amount is paisa-exact rupees for the FBR payload and the
// "Rounding" line on the receipt.
type Totals struct {
	Lines              []LineOut `json:"lines"`
	Subtotal           float64   `json:"subtotal"`
	DiscountAmount     float64   `json:"discount_amount"`
	TaxRate            float64   `json:"tax_rate"`
	TaxAmount          float64   `json:"tax_amount"`
	FurtherTaxAmount   float64   `json:"further_tax_amount"`
	TotalAmount        float64   `json:"total_amount"`
	RoundingAdjustment float64   `json:"rounding_adjustment"`
	TotalPayable       int64     `json:"total_payable"`
}

// Paisa converts a rupee float to integer paisa (port of the retail POS's
// moneyPaisa, backend/internal/handlers/payments.go). math.Round rounds
// half away from zero; every value this package feeds it is clamped ≥ 0
// (quantities and rates are validated positive, discounts are clamped ≥ 0
// below), so "away from zero" and "half up" coincide throughout this file.
func Paisa(v float64) int64 {
	return int64(math.Round(v * 100))
}

// Round2 is round-half-up on the paisa integer, not on the float directly:
// math.Floor(v*100+0.5)/100 is not safe here (it is not "half up" for a
// negative v, and the "+0.5" step can itself lose a ULP right at a .xx5
// boundary). Going through Paisa keeps rounding to a single int64 step.
// Callers only ever pass non-negative money into this function.
func Round2(v float64) float64 {
	return float64(Paisa(v)) / 100
}

// validQuantity accepts a positive quantity with at most 4 decimal places.
// The till stores net weight to 3 dp, but amount-entry mode computes
// qty = round3(amount ÷ rate) and the float64 result can carry residual
// error past the 3rd place; checking against 4 dp (not 3) gives that
// headroom while still catching genuinely malformed input.
func validQuantity(q float64) bool {
	if !(q > 0) || math.IsInf(q, 0) {
		return false
	}
	scaled := q * 10000
	return math.Abs(scaled-math.Round(scaled)) < 1e-6
}

func validRate(r float64) bool {
	return r > 0 && !math.IsInf(r, 0) && !math.IsNaN(r)
}

// ComputeTotals implements spec §6.3 exactly. See the package doc for the
// paisa-arithmetic rationale.
func ComputeTotals(in Input) (Totals, error) {
	if len(in.Lines) == 0 {
		return Totals{}, ErrNoLines
	}

	lineTotalsPaisa := make([]int64, len(in.Lines))
	var subtotalPaisa int64
	for i, l := range in.Lines {
		if !validQuantity(l.Quantity) {
			return Totals{}, ErrInvalidQuantity
		}
		if !validRate(l.Rate) {
			return Totals{}, ErrInvalidRate
		}
		p := Paisa(l.Quantity * l.Rate)
		lineTotalsPaisa[i] = p
		subtotalPaisa += p
	}

	// discount = pct ? round2(subtotal × pct/100) : min(amount, subtotal),
	// both clamped ≥ 0. The amount branch's min() already bounds it above by
	// subtotal; the same upper clamp is applied to the percent branch too so
	// a stray pct > 100 can never manufacture negative taxable money below —
	// the algorithm's "clamped ≥ 0" line implies a shared final clamp, and
	// this is the minimal symmetric reading of it.
	var discountPaisa int64
	if in.DiscountPercent != nil {
		pct := *in.DiscountPercent
		discountPaisa = Paisa(float64(subtotalPaisa) / 100 * pct / 100)
	} else {
		discountPaisa = Paisa(in.DiscountAmount)
	}
	if discountPaisa < 0 {
		discountPaisa = 0
	}
	if discountPaisa > subtotalPaisa {
		discountPaisa = subtotalPaisa
	}

	// Pro-rata line discount: floor per line for all but the last, last line
	// absorbs the remainder so the lines sum to discountPaisa exactly. If
	// subtotal is 0 (every line rounded to 0 paisa), every share is 0.
	lineDiscountsPaisa := make([]int64, len(in.Lines))
	if subtotalPaisa > 0 {
		var allocated int64
		for i := 0; i < len(in.Lines)-1; i++ {
			lineDiscountsPaisa[i] = (discountPaisa * lineTotalsPaisa[i]) / subtotalPaisa
			allocated += lineDiscountsPaisa[i]
		}
		lineDiscountsPaisa[len(in.Lines)-1] = discountPaisa - allocated
	}

	lines := make([]LineOut, len(in.Lines))
	var taxPaisa, taxableSumPaisa int64
	for i, l := range in.Lines {
		taxablePaisa := lineTotalsPaisa[i] - lineDiscountsPaisa[i]
		taxableSumPaisa += taxablePaisa
		lineTaxPaisa := Paisa(float64(taxablePaisa) / 100 * in.TaxRate)
		taxPaisa += lineTaxPaisa
		lines[i] = LineOut{
			Quantity:     l.Quantity,
			UnitPrice:    l.Rate,
			LineTotal:    float64(lineTotalsPaisa[i]) / 100,
			LineDiscount: float64(lineDiscountsPaisa[i]) / 100,
			LineTaxable:  float64(taxablePaisa) / 100,
			LineTax:      float64(lineTaxPaisa) / 100,
		}
	}

	// further_tax = round2(Σ taxable × further_tax_rate), only when the
	// buyer is unregistered (§7.5).
	var furtherTaxPaisa int64
	if !in.BuyerRegistered {
		furtherTaxPaisa = Paisa(float64(taxableSumPaisa) / 100 * in.FurtherTaxRate)
	}

	totalAmountPaisa := taxableSumPaisa + taxPaisa + furtherTaxPaisa
	totalPayable := roundHalfUpToRupee(totalAmountPaisa)
	roundingAdjPaisa := totalPayable*100 - totalAmountPaisa

	return Totals{
		Lines:              lines,
		Subtotal:           float64(subtotalPaisa) / 100,
		DiscountAmount:     float64(discountPaisa) / 100,
		TaxRate:            in.TaxRate,
		TaxAmount:          float64(taxPaisa) / 100,
		FurtherTaxAmount:   float64(furtherTaxPaisa) / 100,
		TotalAmount:        float64(totalAmountPaisa) / 100,
		RoundingAdjustment: float64(roundingAdjPaisa) / 100,
		TotalPayable:       totalPayable,
	}, nil
}

// roundHalfUpToRupee rounds a paisa amount to the nearest whole rupee, ties
// up (spec D9: Rs 3,908.75 → 3,909; Rs 0.50 → 1). Done in pure int64
// arithmetic — no float division — so the one figure the customer actually
// pays never depends on float rounding mode. paisa is never negative for any
// value this package produces (taxable/tax/further-tax are all ≥ 0); the
// negative branch is defensive only.
func roundHalfUpToRupee(paisa int64) int64 {
	if paisa >= 0 {
		return (paisa + 50) / 100
	}
	return -((-paisa + 50) / 100)
}

// TaxRateFor reads tax_rate_<tender> from settings (as loaded by
// settings.Load: raw JSON values, fractions like 0.18). Tax rate for credit
// falls back to the cash rate when tax_rate_credit is null or absent — the
// one key the settings package allows to be null (spec §6.3).
func TaxRateFor(tender string, s map[string]json.RawMessage) float64 {
	key := "tax_rate_" + strings.ToLower(strings.TrimSpace(tender))
	if key == "tax_rate_credit" {
		if v, ok := settingFloat(s, key); ok {
			return v
		}
		return TaxRateFor("cash", s)
	}
	v, _ := settingFloat(s, key)
	return v
}

// settingFloat reads a numeric setting, treating a missing key or the JSON
// literal null as "not set" (ok=false) rather than a hard error — the
// caller decides the fallback.
func settingFloat(s map[string]json.RawMessage, key string) (float64, bool) {
	raw, present := s[key]
	if !present {
		return 0, false
	}
	if strings.TrimSpace(string(raw)) == "null" || len(strings.TrimSpace(string(raw))) == 0 {
		return 0, false
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}
