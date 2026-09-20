package pricing

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/google/uuid"
)

// fixtureCase mirrors one entry of testdata/pricing_fixture.json. expected
// is nil for the one case that expects an error (expectError set instead).
type fixtureCase struct {
	Name        string          `json:"name"`
	Input       Input           `json:"input"`
	Expected    *Totals         `json:"expected"`
	ExpectError string          `json:"expect_error"`
	Raw         json.RawMessage `json:"-"`
}

func loadFixture(t *testing.T) []fixtureCase {
	t.Helper()
	data, err := os.ReadFile("testdata/pricing_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(cases) != 14 {
		t.Fatalf("expected 14 fixture cases, got %d", len(cases))
	}
	return cases
}

const epsilon = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func assertLineEqual(t *testing.T, i int, got, want LineOut) {
	t.Helper()
	if !approxEqual(got.Quantity, want.Quantity) {
		t.Errorf("line %d Quantity = %v, want %v", i, got.Quantity, want.Quantity)
	}
	if !approxEqual(got.UnitPrice, want.UnitPrice) {
		t.Errorf("line %d UnitPrice = %v, want %v", i, got.UnitPrice, want.UnitPrice)
	}
	if !approxEqual(got.LineTotal, want.LineTotal) {
		t.Errorf("line %d LineTotal = %v, want %v", i, got.LineTotal, want.LineTotal)
	}
	if !approxEqual(got.LineDiscount, want.LineDiscount) {
		t.Errorf("line %d LineDiscount = %v, want %v", i, got.LineDiscount, want.LineDiscount)
	}
	if !approxEqual(got.LineTaxable, want.LineTaxable) {
		t.Errorf("line %d LineTaxable = %v, want %v", i, got.LineTaxable, want.LineTaxable)
	}
	if !approxEqual(got.LineTax, want.LineTax) {
		t.Errorf("line %d LineTax = %v, want %v", i, got.LineTax, want.LineTax)
	}
}

func TestComputeTotals_Fixture(t *testing.T) {
	cases := loadFixture(t)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := ComputeTotals(c.Input)
			if c.ExpectError != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil (totals=%+v)", c.ExpectError, got)
				}
				if err.Error() != c.ExpectError {
					t.Fatalf("expected error %q, got %q", c.ExpectError, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.Expected == nil {
				t.Fatalf("fixture case %q has no expected totals and no expect_error", c.Name)
			}
			want := *c.Expected
			if len(got.Lines) != len(want.Lines) {
				t.Fatalf("got %d lines, want %d", len(got.Lines), len(want.Lines))
			}
			for i := range want.Lines {
				assertLineEqual(t, i, got.Lines[i], want.Lines[i])
			}
			if !approxEqual(got.Subtotal, want.Subtotal) {
				t.Errorf("Subtotal = %v, want %v", got.Subtotal, want.Subtotal)
			}
			if !approxEqual(got.DiscountAmount, want.DiscountAmount) {
				t.Errorf("DiscountAmount = %v, want %v", got.DiscountAmount, want.DiscountAmount)
			}
			if !approxEqual(got.TaxRate, want.TaxRate) {
				t.Errorf("TaxRate = %v, want %v", got.TaxRate, want.TaxRate)
			}
			if !approxEqual(got.TaxAmount, want.TaxAmount) {
				t.Errorf("TaxAmount = %v, want %v", got.TaxAmount, want.TaxAmount)
			}
			if !approxEqual(got.FurtherTaxAmount, want.FurtherTaxAmount) {
				t.Errorf("FurtherTaxAmount = %v, want %v", got.FurtherTaxAmount, want.FurtherTaxAmount)
			}
			if !approxEqual(got.TotalAmount, want.TotalAmount) {
				t.Errorf("TotalAmount = %v, want %v", got.TotalAmount, want.TotalAmount)
			}
			if !approxEqual(got.RoundingAdjustment, want.RoundingAdjustment) {
				t.Errorf("RoundingAdjustment = %v, want %v", got.RoundingAdjustment, want.RoundingAdjustment)
			}
			if got.TotalPayable != want.TotalPayable {
				t.Errorf("TotalPayable = %v, want %v", got.TotalPayable, want.TotalPayable)
			}
		})
	}
}

func TestComputeTotals_NoLines(t *testing.T) {
	_, err := ComputeTotals(Input{Lines: nil, TaxRate: 0.18})
	if !errors.Is(err, ErrNoLines) {
		t.Fatalf("got %v, want ErrNoLines", err)
	}
}

func TestComputeTotals_NegativeQuantity(t *testing.T) {
	_, err := ComputeTotals(Input{
		Lines: []LineIn{{ProductID: uuid.New(), Quantity: -1, Rate: 265}},
	})
	if !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("got %v, want ErrInvalidQuantity", err)
	}
}

func TestComputeTotals_TooManyDecimalPlaces(t *testing.T) {
	_, err := ComputeTotals(Input{
		Lines: []LineIn{{ProductID: uuid.New(), Quantity: 1.23456, Rate: 265}},
	})
	if !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("got %v, want ErrInvalidQuantity", err)
	}
}

func TestComputeTotals_FourDecimalPlacesAllowed(t *testing.T) {
	// The till captures 3 dp, but amount-entry mode computes qty = round3(amount/rate);
	// stored as float64 that can carry residual error at the 4th place. ComputeTotals
	// must not reject a legitimate 3 dp quantity just because the float representation
	// shows noise past the 4th digit, so the cutoff is 4 dp, not 3.
	_, err := ComputeTotals(Input{
		Lines: []LineIn{{ProductID: uuid.New(), Quantity: 1.2345, Rate: 265}},
	})
	if err != nil {
		t.Fatalf("4 dp quantity must be accepted, got %v", err)
	}
}

func TestComputeTotals_InvalidRate(t *testing.T) {
	_, err := ComputeTotals(Input{
		Lines: []LineIn{{ProductID: uuid.New(), Quantity: 1, Rate: 0}},
	})
	if !errors.Is(err, ErrInvalidRate) {
		t.Fatalf("got %v, want ErrInvalidRate", err)
	}
	_, err = ComputeTotals(Input{
		Lines: []LineIn{{ProductID: uuid.New(), Quantity: 1, Rate: -5}},
	})
	if !errors.Is(err, ErrInvalidRate) {
		t.Fatalf("got %v, want ErrInvalidRate", err)
	}
}

// I1 review fix: negative discount amount/percent and negative tax rates
// must be rejected, not silently clamped or accepted.
func TestComputeTotals_InvalidDiscount(t *testing.T) {
	_, err := ComputeTotals(Input{
		Lines:          []LineIn{{ProductID: uuid.New(), Quantity: 1, Rate: 100}},
		DiscountAmount: -1,
	})
	if !errors.Is(err, ErrInvalidDiscount) {
		t.Fatalf("negative discount_amount: got %v, want ErrInvalidDiscount", err)
	}

	negPct := -5.0
	_, err = ComputeTotals(Input{
		Lines:           []LineIn{{ProductID: uuid.New(), Quantity: 1, Rate: 100}},
		DiscountPercent: &negPct,
	})
	if !errors.Is(err, ErrInvalidDiscount) {
		t.Fatalf("negative discount_percent: got %v, want ErrInvalidDiscount", err)
	}
}

func TestComputeTotals_InvalidTaxRate(t *testing.T) {
	_, err := ComputeTotals(Input{
		Lines:   []LineIn{{ProductID: uuid.New(), Quantity: 1, Rate: 100}},
		TaxRate: -0.01,
	})
	if !errors.Is(err, ErrInvalidTaxRate) {
		t.Fatalf("negative tax_rate: got %v, want ErrInvalidTaxRate", err)
	}

	_, err = ComputeTotals(Input{
		Lines:          []LineIn{{ProductID: uuid.New(), Quantity: 1, Rate: 100}},
		FurtherTaxRate: -0.01,
	})
	if !errors.Is(err, ErrInvalidTaxRate) {
		t.Fatalf("negative further_tax_rate: got %v, want ErrInvalidTaxRate", err)
	}
}

// I2 review fix: with enough lines and a steep enough discount, a "last
// line absorbs the remainder" allocation can push that line's discount past
// its own line_total, making line_taxable negative. The largest-remainder
// method must never do that: every line_discount stays within its own
// line_total, and the shares still sum exactly to the invoice discount.
// (Same numbers as the fixture's three_lines_heavy_discount case, asserted
// here more directly as a standalone regression.)
func TestComputeTotals_ThreeLinesNeverGoesNegative(t *testing.T) {
	got, err := ComputeTotals(Input{
		Lines: []LineIn{
			{ProductID: uuid.New(), Quantity: 10.0, Rate: 265},
			{ProductID: uuid.New(), Quantity: 5.0, Rate: 265},
			{ProductID: uuid.New(), Quantity: 0.001, Rate: 265},
		},
		DiscountAmount: 3950.00,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sumDiscounts float64
	for i, l := range got.Lines {
		if l.LineDiscount > l.LineTotal+epsilon {
			t.Errorf("line %d LineDiscount %v exceeds LineTotal %v", i, l.LineDiscount, l.LineTotal)
		}
		if l.LineTaxable < -epsilon {
			t.Errorf("line %d LineTaxable %v is negative", i, l.LineTaxable)
		}
		sumDiscounts += l.LineDiscount
	}
	if !approxEqual(sumDiscounts, got.DiscountAmount) {
		t.Errorf("Σ LineDiscount = %v, want DiscountAmount %v", sumDiscounts, got.DiscountAmount)
	}
}

// C1 review fix regression: this exact case (3.260 kg × 265.00, 15% discount,
// 18% tax) previously computed a different TotalPayable in the TS mirror
// (867) than in Go (866) because the two used different float expressions
// for the percent-discount calculation. Both now run the same scaled-integer
// algorithm and must agree; this pins Go's own side of that agreement.
func TestComputeTotals_C1PercentDiscountRegression(t *testing.T) {
	pct := 15.0
	got, err := ComputeTotals(Input{
		Lines:           []LineIn{{ProductID: uuid.New(), Quantity: 3.260, Rate: 265.00}},
		DiscountPercent: &pct,
		TaxRate:         0.18,
		BuyerRegistered: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalPayable != 866 {
		t.Errorf("TotalPayable = %d, want 866", got.TotalPayable)
	}
	if !approxEqual(got.RoundingAdjustment, -0.49) {
		t.Errorf("RoundingAdjustment = %v, want -0.49", got.RoundingAdjustment)
	}
}

func TestRound2(t *testing.T) {
	cases := []struct {
		in, want float64
		note     string
	}{
		{3908.745, 3908.75, "half-up at the paisa boundary (3908.745*100=390874.5)"},
		{0.005, 0.01, "0.5 paisa rounds up (documents the half-up rule)"},
		{3312.50, 3312.50, "already exact, unchanged"},
		{0, 0, "zero"},
		// I1 review fix: these two are exact half-paisa ties whose float64
		// product lands one ULP LOW of the true value (0.22499999999999998
		// and 2.3849999999999998 respectively), so the naive
		// math.Round(v*100) path floats DOWN to 0.22/2.38 instead of the
		// correct half-up 0.23/2.39. Round2 must get these right by scaling
		// to micro-rupees (v*1e6) before rounding to an integer, where the
		// same float error is negligible next to the true integer value.
		{1.25 * 0.18, 0.23, "1.25×0.18=0.225 exact tie, float64 product is one ULP low"},
		{0.009 * 265, 2.39, "0.009×265=2.385 exact tie, float64 product is one ULP low"},
	}
	for _, c := range cases {
		got := Round2(c.in)
		if !approxEqual(got, c.want) {
			t.Errorf("Round2(%v) [%s] = %v, want %v", c.in, c.note, got, c.want)
		}
	}
}

func TestPaisa(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{3312.50, 331250},
		{0.50, 50},
		{265000.00, 26500000},
		{0, 0},
	}
	for _, c := range cases {
		if got := Paisa(c.in); got != c.want {
			t.Errorf("Paisa(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestTaxRateFor covers fixture case 8 (credit_rate_null_falls_back), kept
// out of the shared JSON fixture per the brief since it exercises settings
// lookup rather than ComputeTotals.
func TestTaxRateFor(t *testing.T) {
	settings := map[string]json.RawMessage{
		"tax_rate_cash":   json.RawMessage(`0.18`),
		"tax_rate_card":   json.RawMessage(`0.18`),
		"tax_rate_online": json.RawMessage(`0.18`),
		"tax_rate_credit": json.RawMessage(`null`),
	}
	if got := TaxRateFor("credit", settings); !approxEqual(got, 0.18) {
		t.Errorf("credit with null tax_rate_credit = %v, want cash rate 0.18", got)
	}
	if got := TaxRateFor("cash", settings); !approxEqual(got, 0.18) {
		t.Errorf("cash = %v, want 0.18", got)
	}

	settings["tax_rate_credit"] = json.RawMessage(`0.05`)
	if got := TaxRateFor("credit", settings); !approxEqual(got, 0.05) {
		t.Errorf("credit with tax_rate_credit set = %v, want 0.05", got)
	}

	settings["tax_rate_card"] = json.RawMessage(`0`)
	if got := TaxRateFor("card", settings); !approxEqual(got, 0) {
		t.Errorf("card = %v, want 0", got)
	}
}

// TestTaxRateFor_AbsentKeyAndUnknownTender: a settings map that simply
// doesn't have the key (not just a null value), and a tender that isn't
// cash/card/online/credit, both return 0 — fail closed, never silently
// fall back to the cash rate for a tender nobody configured.
func TestTaxRateFor_AbsentKeyAndUnknownTender(t *testing.T) {
	empty := map[string]json.RawMessage{}
	if got := TaxRateFor("cash", empty); got != 0 {
		t.Errorf("cash with no tax_rate_cash key = %v, want 0", got)
	}

	settings := map[string]json.RawMessage{
		"tax_rate_cash": json.RawMessage(`0.18`),
	}
	if got := TaxRateFor("bank_transfer", settings); got != 0 {
		t.Errorf(`unknown tender "bank_transfer" = %v, want 0 (not the cash rate)`, got)
	}
	if got := TaxRateFor("", settings); got != 0 {
		t.Errorf("empty tender = %v, want 0", got)
	}
}
