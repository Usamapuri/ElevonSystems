import { readFileSync } from 'fs'
import path from 'path'
import { describe, expect, it } from 'vitest'
import { computeTotals, paisa, round2, taxRateFor, PricingError, type Input, type Totals } from './pricing'

interface FixtureCase {
  name: string
  input: Input
  expected?: Totals
  expect_error?: string
}

function loadFixture(): FixtureCase[] {
  const raw = readFileSync(path.resolve(__dirname, './pricing_fixture.json'), 'utf-8')
  return JSON.parse(raw) as FixtureCase[]
}

const EPS = 1e-9
function approx(a: number, b: number, label: string) {
  expect(Math.abs(a - b), `${label}: ${a} !== ${b}`).toBeLessThan(EPS)
}

function assertTotalsEqual(got: Totals, want: Totals) {
  expect(got.lines.length).toBe(want.lines.length)
  want.lines.forEach((wantLine, i) => {
    const gotLine = got.lines[i]
    approx(gotLine.quantity, wantLine.quantity, `line ${i} quantity`)
    approx(gotLine.unit_price, wantLine.unit_price, `line ${i} unit_price`)
    approx(gotLine.line_total, wantLine.line_total, `line ${i} line_total`)
    approx(gotLine.line_discount, wantLine.line_discount, `line ${i} line_discount`)
    approx(gotLine.line_taxable, wantLine.line_taxable, `line ${i} line_taxable`)
    approx(gotLine.line_tax, wantLine.line_tax, `line ${i} line_tax`)
  })
  approx(got.subtotal, want.subtotal, 'subtotal')
  approx(got.discount_amount, want.discount_amount, 'discount_amount')
  approx(got.tax_rate, want.tax_rate, 'tax_rate')
  approx(got.tax_amount, want.tax_amount, 'tax_amount')
  approx(got.further_tax_amount, want.further_tax_amount, 'further_tax_amount')
  approx(got.total_amount, want.total_amount, 'total_amount')
  approx(got.rounding_adjustment, want.rounding_adjustment, 'rounding_adjustment')
  expect(got.total_payable).toBe(want.total_payable)
}

describe('computeTotals — fixture', () => {
  const cases = loadFixture()
  it('loads exactly 14 fixture cases', () => {
    expect(cases.length).toBe(14)
  })
  for (const c of cases) {
    it(c.name, () => {
      if (c.expect_error) {
        expect(() => computeTotals(c.input)).toThrow(PricingError)
        try {
          computeTotals(c.input)
        } catch (err) {
          expect(err).toBeInstanceOf(PricingError)
          expect((err as PricingError).code).toBe(c.expect_error)
        }
        return
      }
      if (!c.expected) throw new Error(`fixture case ${c.name} has no expected totals and no expect_error`)
      const got = computeTotals(c.input)
      assertTotalsEqual(got, c.expected)
    })
  }
})

describe('computeTotals — errors', () => {
  it('rejects an empty line list', () => {
    expect(() => computeTotals({ lines: [], discount_amount: 0, discount_percent: null, tax_rate: 0.18, further_tax_rate: 0, buyer_registered: true })).toThrow(
      PricingError,
    )
  })
  it('rejects a negative quantity', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: -1, rate: 265 }],
        discount_amount: 0,
        discount_percent: null,
        tax_rate: 0.18,
        further_tax_rate: 0,
        buyer_registered: true,
      }),
    ).toThrow(PricingError)
  })
  it('rejects a quantity with more than 4 decimal places', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: 1.23456, rate: 265 }],
        discount_amount: 0,
        discount_percent: null,
        tax_rate: 0.18,
        further_tax_rate: 0,
        buyer_registered: true,
      }),
    ).toThrow(PricingError)
  })
  it('accepts a quantity with exactly 4 decimal places', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: 1.2345, rate: 265 }],
        discount_amount: 0,
        discount_percent: null,
        tax_rate: 0.18,
        further_tax_rate: 0,
        buyer_registered: true,
      }),
    ).not.toThrow()
  })
  it('rejects a zero or negative rate', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: 1, rate: 0 }],
        discount_amount: 0,
        discount_percent: null,
        tax_rate: 0.18,
        further_tax_rate: 0,
        buyer_registered: true,
      }),
    ).toThrow(PricingError)
  })

  // I1 review fix: negative discount amount/percent and negative tax rates
  // must be rejected, not silently clamped or accepted.
  it('rejects a negative discount_amount', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: 1, rate: 100 }],
        discount_amount: -1,
        discount_percent: null,
        tax_rate: 0,
        further_tax_rate: 0,
        buyer_registered: true,
      }),
    ).toThrow(PricingError)
  })
  it('rejects a negative discount_percent', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: 1, rate: 100 }],
        discount_amount: 0,
        discount_percent: -5,
        tax_rate: 0,
        further_tax_rate: 0,
        buyer_registered: true,
      }),
    ).toThrow(PricingError)
  })
  it('rejects a negative tax_rate', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: 1, rate: 100 }],
        discount_amount: 0,
        discount_percent: null,
        tax_rate: -0.01,
        further_tax_rate: 0,
        buyer_registered: true,
      }),
    ).toThrow(PricingError)
  })
  it('rejects a negative further_tax_rate', () => {
    expect(() =>
      computeTotals({
        lines: [{ product_id: 'x', quantity: 1, rate: 100 }],
        discount_amount: 0,
        discount_percent: null,
        tax_rate: 0,
        further_tax_rate: -0.01,
        buyer_registered: true,
      }),
    ).toThrow(PricingError)
  })
})

// I2 review fix: with enough lines and a steep enough discount, a "last
// line absorbs the remainder" allocation can push that line's discount past
// its own line_total, making line_taxable negative. The largest-remainder
// method must never do that: every line_discount stays within its own
// line_total, and the shares still sum exactly to the invoice discount.
// (Same numbers as the fixture's three_lines_heavy_discount case, asserted
// here more directly as a standalone regression.)
describe('computeTotals — three lines never go negative', () => {
  it('keeps every line_discount within its own line_total', () => {
    const got = computeTotals({
      lines: [
        { product_id: 'a', quantity: 10.0, rate: 265 },
        { product_id: 'b', quantity: 5.0, rate: 265 },
        { product_id: 'c', quantity: 0.001, rate: 265 },
      ],
      discount_amount: 3950.0,
      discount_percent: null,
      tax_rate: 0,
      further_tax_rate: 0,
      buyer_registered: true,
    })
    let sumDiscounts = 0
    for (const line of got.lines) {
      expect(line.line_discount).toBeLessThanOrEqual(line.line_total + EPS)
      expect(line.line_taxable).toBeGreaterThanOrEqual(-EPS)
      sumDiscounts += line.line_discount
    }
    approx(sumDiscounts, got.discount_amount, 'sum of line discounts')
  })
})

// C1 review fix regression: this exact case (3.260 kg × 265.00, 15%
// discount, 18% tax) previously computed a different total_payable in the
// TS mirror (867) than in Go (866) because the two used different float
// expressions for the percent-discount calculation. Both now run the same
// scaled-integer algorithm and must agree; this pins TS's own side of that
// agreement.
describe('computeTotals — C1 percent discount regression', () => {
  it('matches Go: 3.260 kg × 265.00, 15% discount, 18% tax → payable 866', () => {
    const got = computeTotals({
      lines: [{ product_id: 'x', quantity: 3.26, rate: 265.0 }],
      discount_amount: 0,
      discount_percent: 15,
      tax_rate: 0.18,
      further_tax_rate: 0,
      buyer_registered: true,
    })
    expect(got.total_payable).toBe(866)
    approx(got.rounding_adjustment, -0.49, 'rounding_adjustment')
  })
})

describe('round2 / paisa', () => {
  it('round2 is half-up at the paisa boundary', () => {
    expect(round2(3908.745)).toBeCloseTo(3908.75, 9)
    expect(round2(0.005)).toBeCloseTo(0.01, 9)
    expect(round2(3312.5)).toBeCloseTo(3312.5, 9)
  })
  // I1 review fix: these two are exact half-paisa ties whose float64
  // product lands one ULP LOW of the true value (0.22499999999999998 and
  // 2.3849999999999998 respectively), so the naive Math.round(v*100) path
  // floats DOWN to 0.22/2.38 instead of the correct half-up 0.23/2.39.
  // round2 must get these right by scaling to micro-rupees (v*1e6) before
  // rounding to an integer, where the same float error is negligible next
  // to the true integer value.
  it('round2 resolves ties that float DOWN under naive v*100 rounding', () => {
    expect(round2(1.25 * 0.18)).toBeCloseTo(0.23, 9) // exact tie 0.225
    expect(round2(0.009 * 265)).toBeCloseTo(2.39, 9) // exact tie 2.385
  })
  it('paisa converts rupees to integer paisa', () => {
    expect(paisa(3312.5)).toBe(331250)
    expect(paisa(0.5)).toBe(50)
    expect(paisa(265000)).toBe(26500000)
  })
})

// Fixture case 8 (credit_rate_null_falls_back), kept out of the shared JSON
// fixture per the brief since it exercises settings lookup, not
// computeTotals.
describe('taxRateFor', () => {
  const settings = { tax_rate_cash: 0.18, tax_rate_card: 0.18, tax_rate_online: 0.18, tax_rate_credit: null }
  it('credit falls back to cash when tax_rate_credit is null', () => {
    expect(taxRateFor('credit', settings)).toBe(0.18)
  })
  it('credit uses its own rate when set', () => {
    expect(taxRateFor('credit', { ...settings, tax_rate_credit: 0.05 })).toBe(0.05)
  })
  it('reads cash/card/online directly', () => {
    expect(taxRateFor('cash', settings)).toBe(0.18)
    expect(taxRateFor('card', { ...settings, tax_rate_card: 0 })).toBe(0)
    expect(taxRateFor('online', settings)).toBe(0.18)
  })
  // Unknown tenders fail closed to 0, never silently the cash rate — a
  // tender nobody configured must never get charged tax on cash's behalf.
  it('returns 0 for an unknown tender, not the cash rate', () => {
    expect(taxRateFor('bank_transfer', settings)).toBe(0)
    expect(taxRateFor('', settings)).toBe(0)
  })
})
