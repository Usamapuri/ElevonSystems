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
	if len(cases) != 9 {
		t.Fatalf("expected 9 fixture cases, got %d", len(cases))
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

func TestRound2(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{3908.745, 3908.75}, // half-up at the paisa boundary (3908.745*100=390874.5)
		{0.005, 0.01},       // 0.5 paisa rounds up (documents the half-up rule)
		{3312.50, 3312.50},  // already exact, unchanged
		{0, 0},
	}
	for _, c := range cases {
		got := Round2(c.in)
		if !approxEqual(got, c.want) {
			t.Errorf("Round2(%v) = %v, want %v", c.in, got, c.want)
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
