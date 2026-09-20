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
  it('loads exactly 9 fixture cases', () => {
    expect(cases.length).toBe(9)
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
})

describe('round2 / paisa', () => {
  it('round2 is half-up at the paisa boundary', () => {
    expect(round2(3908.745)).toBeCloseTo(3908.75, 9)
    expect(round2(0.005)).toBeCloseTo(0.01, 9)
    expect(round2(3312.5)).toBeCloseTo(3312.5, 9)
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
})
