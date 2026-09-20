// Package pricing is the one money function (spec §6.3): every invoice total
// is computed server-side from products.rate and the submitted quantities,
// never trusted from the client. Every amount that could land on an exact
// half-paisa (or half-rupee) tie is computed via scaled int64 arithmetic and
// halfUpDiv, never by rounding a float64 multiplication result directly —
// see the halfUpDiv doc comment for why that distinction matters. lib/
// pricing.ts is the TS mirror (same algorithm, BigInt in place of int64
// where a product could exceed 2^53); both suites load the same JSON
// fixture (testdata/pricing_fixture.json / frontend/src/lib/
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
	"sort"
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
	ErrInvalidDiscount = errors.New("invalid_discount")
	ErrInvalidTaxRate  = errors.New("invalid_tax_rate")
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
// "Rounding" line on the receipt. RoundingAdjustment's achievable range
// under half-up-ties-up is −0.49 … +0.50 (paisa fraction 49 rounds down to
// −0.49, paisa fraction 50 rounds up to +0.50) — the design spec's DB
// column comment and the original task brief both say "−0.50 … +0.49",
// which is off by a cent at both ends; see fixture cases
// `tie_rounds_up_line_total` (−0.39) and `rounding_half_up` (+0.50) for the
// achieved bounds in practice, and the report for the full note.
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
// moneyPaisa, backend/internal/handlers/payments.go): a single float64
// multiplication (v*100) rounded with math.Round. This is kept exactly as
// the original brief specified it, and ComputeTotals no longer calls it for
// any value that could land on an exact tie (see halfUpDiv) — but a caller
// that hands Paisa a raw float which happens to be an exact x.xx5 tie (e.g.
// a value computed elsewhere as qty*rate without going through
// ComputeTotals) can still be misrounded by a single ULP of float
// multiplication error, the same class of bug fixed in Round2 below. Prefer
// Round2 for any float that might be an exact 2dp tie; Paisa is for values
// already known to be clean (a parsed rupee amount, a rate straight off
// products.rate).
func Paisa(v float64) int64 {
	return int64(math.Round(v * 100))
}

// Round2 rounds a float64 to 2dp, half-up, robust to the float
// multiplication error that can flip an exact tie the wrong way. Naively
// rounding at the target scale (math.Round(v*100), or the even less safe
// math.Floor(v*100+0.5)/100 — not safe for a negative v either) fails on
// inputs like 1.25*0.18: the mathematically exact product is 0.225 (an
// exact half-paisa tie), but the float64 result of that multiplication is
// 0.22499999999999998 (one ULP low), so v*100 evaluates to
// 22.499999999999996 and rounds DOWN to 22 (0.22) instead of up to 23
// (0.23).
//
// The fix scales to a much finer grid first (v*1e6, micro-rupees) before
// rounding to an integer: the same ~1e-15 relative float error is now
// utterly negligible next to the 0.5 threshold at THIS scale (225000 vs.
// the computed 224999.99999999997 — off by 3e-11, nowhere near a tie), so
// round(v*1e6) reliably recovers the true integer value. Any genuine tie at
// the 2dp/paisa level is then resolved by an exact integer division
// (halfUpDiv, no floats involved) rather than by float rounding.
//
// Callers only ever pass non-negative money into this function (discounts
// are clamped ≥ 0 elsewhere in this package); the sign handling below is
// defensive.
func Round2(v float64) float64 {
	microRupees := int64(math.Round(v * 1e6))
	neg := microRupees < 0
	if neg {
		microRupees = -microRupees
	}
	paisaVal := halfUpDiv(microRupees, 10000)
	if neg {
		paisaVal = -paisaVal
	}
	return float64(paisaVal) / 100
}

// halfUpDiv divides two non-negative int64s, rounding the quotient half up:
// (n + d/2) / d in integer arithmetic. d is always one of this package's
// fixed scale factors (10000, 1e6, 1e8), all even, so d/2 is exact and this
// never touches a float. This is the one place a genuine tie (the
// mathematically exact result lands precisely halfway between two integers
// at the target scale) gets resolved — deterministically, the same way in
// Go and TS, independent of any float representation question.
func halfUpDiv(n, d int64) int64 {
	return (n + d/2) / d
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

// validNonNegative rejects NaN, ±Inf and negative values — the shared check
// for tax rates and discount amount/percent (ErrInvalidTaxRate /
// ErrInvalidDiscount). Zero is allowed (a 0% tax rate or a 0 discount are
// both ordinary, valid inputs).
func validNonNegative(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// ComputeTotals implements spec §6.3. Every amount that could land on an
// exact tie is computed via scaled int64 arithmetic (see halfUpDiv), so the
// result never depends on float64 multiplication rounding the way a direct
// Paisa(qty*rate)-style computation can (see Round2's doc comment for a
// worked example of that failure mode). The three scale factors are:
// quantity ×10000 (q), rate ×100 (r), and tax/further-tax/discount-percent
// ×1e6 (t, f, p) — chosen so every product below lands on a whole multiple
// of the target unit's reciprocal, keeping halfUpDiv's tie-break exact.
func ComputeTotals(in Input) (Totals, error) {
	if len(in.Lines) == 0 {
		return Totals{}, ErrNoLines
	}
	if !validNonNegative(in.TaxRate) || !validNonNegative(in.FurtherTaxRate) {
		return Totals{}, ErrInvalidTaxRate
	}
	if in.DiscountPercent != nil {
		if !validNonNegative(*in.DiscountPercent) {
			return Totals{}, ErrInvalidDiscount
		}
	} else if !validNonNegative(in.DiscountAmount) {
		return Totals{}, ErrInvalidDiscount
	}

	n := len(in.Lines)
	lineTotalsPaisa := make([]int64, n)
	var subtotalPaisa int64
	for i, l := range in.Lines {
		if !validQuantity(l.Quantity) {
			return Totals{}, ErrInvalidQuantity
		}
		if !validRate(l.Rate) {
			return Totals{}, ErrInvalidRate
		}
		q := int64(math.Round(l.Quantity * 10000))
		r := int64(math.Round(l.Rate * 100))
		// q*r = qty×rate×1e6 (micro-rupees); /10000 collapses that to paisa
		// with an exact half-up tie-break — no float multiplication of qty
		// by rate ever happens.
		lt := halfUpDiv(q*r, 10000)
		lineTotalsPaisa[i] = lt
		subtotalPaisa += lt
	}

	t := int64(math.Round(in.TaxRate * 1e6))
	f := int64(math.Round(in.FurtherTaxRate * 1e6))

	// discount = pct ? min(halfUpDiv(subtotal×p, 1e8), subtotal)
	//               : min(round(amount×100), subtotal), both clamped ≥ 0.
	var rawDiscountPaisa int64
	if in.DiscountPercent != nil {
		p := int64(math.Round(*in.DiscountPercent * 1e6))
		// subtotal×p = subtotalPaisa × pct × 1e6; /1e8 = ×pct/100, i.e. the
		// pct-of-subtotal discount in paisa, half-up.
		rawDiscountPaisa = halfUpDiv(subtotalPaisa*p, 100_000_000)
	} else {
		rawDiscountPaisa = int64(math.Round(in.DiscountAmount * 100))
	}
	discountPaisa := rawDiscountPaisa
	if discountPaisa < 0 {
		discountPaisa = 0
	}
	if discountPaisa > subtotalPaisa {
		discountPaisa = subtotalPaisa
	}

	// Pro-rata line discount via the largest-remainder method: floor each
	// line's exact share, then hand the leftover paisa (discount − Σfloors,
	// always < n) one at a time to the lines with the largest fractional
	// remainder — never to a fixed "last line", which can push that line's
	// discount past its own line_total and make line_taxable negative (a
	// real failure mode with enough lines and a steep enough discount).
	// Every remainder here shares the same denominator (subtotalPaisa), so
	// comparing the numerators directly orders them correctly — no floats.
	// If subtotal is 0 (every line rounded to 0 paisa), every share is 0.
	lineDiscountsPaisa := make([]int64, n)
	if subtotalPaisa > 0 {
		remainders := make([]int64, n)
		var floorSum int64
		for i := 0; i < n; i++ {
			prod := discountPaisa * lineTotalsPaisa[i]
			lineDiscountsPaisa[i] = prod / subtotalPaisa
			remainders[i] = prod % subtotalPaisa
			floorSum += lineDiscountsPaisa[i]
		}
		deficit := discountPaisa - floorSum

		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			return remainders[order[a]] > remainders[order[b]]
		})

		// One pass normally exhausts the deficit (it is always < n given
		// discount ≤ subtotal); a second pass is defensive so the "never
		// exceed a line's own total" cap can never leave paisa undistributed.
		for pass := 0; pass < 2 && deficit > 0; pass++ {
			for _, idx := range order {
				if deficit <= 0 {
					break
				}
				if lineDiscountsPaisa[idx] < lineTotalsPaisa[idx] {
					lineDiscountsPaisa[idx]++
					deficit--
				}
			}
		}
	}

	lines := make([]LineOut, n)
	var taxPaisa, taxableSumPaisa int64
	for i, l := range in.Lines {
		taxablePaisa := lineTotalsPaisa[i] - lineDiscountsPaisa[i] // ≥ 0 by construction
		taxableSumPaisa += taxablePaisa
		lineTaxPaisa := halfUpDiv(taxablePaisa*t, 1_000_000)
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

	// further_tax = halfUpDiv(Σ taxable × further_tax_rate), only when the
	// buyer is unregistered (§7.5).
	var furtherTaxPaisa int64
	if !in.BuyerRegistered {
		furtherTaxPaisa = halfUpDiv(taxableSumPaisa*f, 1_000_000)
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
// one key the settings package allows to be null (spec §6.3). A tender that
// is not cash/card/online/credit (or a settings map simply missing the key)
// returns 0, not the cash rate — fail closed rather than silently charging
// a rate the caller never configured for that tender.
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
