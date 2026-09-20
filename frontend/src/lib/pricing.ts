// The one money function (spec §6.3), mirrored from the Go source of truth
// (backend/internal/pricing/pricing.go) for the live checkout preview only.
// The server recomputes every invoice from products.rate; this file must
// never be trusted for the figure that actually gets charged. Both suites
// load the same JSON fixture (pricing_fixture.json / backend/internal/
// pricing/testdata/pricing_fixture.json) and a parity test in each pins the
// two files byte identical.
//
// Money-changing edits here are customer-visible (CLAUDE.md invariant 8):
// compute before/after examples and get the owner's sign-off before merge.
import type { AppSettings } from '@/types'

/** One cart line: quantity in kg (3 dp from the weight pad; up to 4 dp
 * accepted, see validQuantity), rate per kg (2 dp, always products.rate). */
export interface LineIn {
  product_id: string
  quantity: number
  rate: number
}

/** discount_percent null means the discount was entered as a rupee amount
 * (discount_amount); a number means it was entered as a percentage of the
 * subtotal (5 for 5%, not 0.05) and discount_amount is ignored. tax_rate /
 * further_tax_rate are fractions (0.18, not 18) — resolve with taxRateFor
 * first. */
export interface Input {
  lines: LineIn[]
  discount_amount: number
  discount_percent: number | null
  tax_rate: number
  further_tax_rate: number
  buyer_registered: boolean
}

export interface LineOut {
  quantity: number
  unit_price: number
  line_total: number
  line_discount: number
  line_taxable: number
  line_tax: number
}

/** total_payable is a whole-rupee integer (the figure the customer actually
 * pays and the ledger carries, per spec D9); every other amount is
 * paisa-exact rupees. */
export interface Totals {
  lines: LineOut[]
  subtotal: number
  discount_amount: number
  tax_rate: number
  tax_amount: number
  further_tax_amount: number
  total_amount: number
  rounding_adjustment: number
  total_payable: number
}

export class PricingError extends Error {
  constructor(public code: 'no_lines' | 'invalid_quantity' | 'invalid_rate') {
    super(code)
    this.name = 'PricingError'
  }
}

/** Rupee float → integer paisa (port of the Go source's Paisa, itself a port
 * of the retail POS's moneyPaisa). Every value fed through this file is
 * clamped ≥ 0 (quantities/rates validated positive, discounts clamped ≥ 0),
 * so plain half-up rounding is safe: JS Math.round ties toward +Infinity,
 * which for non-negative inputs is the same as "away from zero". */
export function paisa(v: number): number {
  return Math.round(v * 100)
}

/** Round-half-up on the paisa integer, not the float directly — same
 * rationale as the Go mirror: going through paisa() keeps rounding to a
 * single step instead of a float "+0.5 then floor" that can lose a ULP at
 * a .xx5 boundary. Callers only ever pass non-negative money in. */
export function round2(v: number): number {
  return paisa(v) / 100
}

/** Accepts a positive quantity with at most 4 decimal places. The till
 * stores net weight to 3 dp, but amount-entry mode computes
 * qty = round3(amount ÷ rate) and the float result can carry residual error
 * past the 3rd place; checking against 4 dp (not 3) gives that headroom
 * while still catching genuinely malformed input. */
function validQuantity(q: number): boolean {
  if (!(q > 0) || !Number.isFinite(q)) return false
  const scaled = q * 10000
  return Math.abs(scaled - Math.round(scaled)) < 1e-6
}

function validRate(r: number): boolean {
  return r > 0 && Number.isFinite(r)
}

/** Floor(numPaisa × mulPaisa ÷ denPaisa) done in BigInt so a large invoice
 * never risks the > 2^53 precision loss plain Number multiplication of two
 * paisa-scale integers could hit; Go's mirror gets this for free from
 * int64. Exact for non-negative operands (BigInt division truncates toward
 * zero, which equals floor there). */
function floorDivPaisa(numPaisa: number, mulPaisa: number, denPaisa: number): number {
  return Number((BigInt(numPaisa) * BigInt(mulPaisa)) / BigInt(denPaisa))
}

/** Rounds a paisa amount to the nearest whole rupee, ties up (spec D9:
 * Rs 3,908.75 → 3,909; Rs 0.50 → 1). Pure integer arithmetic, no float
 * division, so the figure the customer actually pays never depends on
 * float rounding. paisaAmount is never negative for any value this file
 * produces; the negative branch is defensive only. */
function roundHalfUpToRupee(paisaAmount: number): number {
  if (paisaAmount >= 0) return Math.floor((paisaAmount + 50) / 100)
  return -Math.floor((-paisaAmount + 50) / 100)
}

/** Implements spec §6.3 exactly. See the file header for the paisa-
 * arithmetic rationale. */
export function computeTotals(input: Input): Totals {
  if (input.lines.length === 0) {
    throw new PricingError('no_lines')
  }

  const lineTotalsPaisa: number[] = []
  let subtotalPaisa = 0
  for (const line of input.lines) {
    if (!validQuantity(line.quantity)) {
      throw new PricingError('invalid_quantity')
    }
    if (!validRate(line.rate)) {
      throw new PricingError('invalid_rate')
    }
    const p = paisa(line.quantity * line.rate)
    lineTotalsPaisa.push(p)
    subtotalPaisa += p
  }

  // discount = pct ? round2(subtotal × pct/100) : min(amount, subtotal),
  // both clamped ≥ 0. The amount branch's min() already bounds it above by
  // subtotal; the same upper clamp is applied to the percent branch too so
  // a stray pct > 100 can never manufacture negative taxable money below.
  let discountPaisa: number
  if (input.discount_percent !== null && input.discount_percent !== undefined) {
    discountPaisa = paisa((subtotalPaisa / 100) * (input.discount_percent / 100))
  } else {
    discountPaisa = paisa(input.discount_amount)
  }
  if (discountPaisa < 0) discountPaisa = 0
  if (discountPaisa > subtotalPaisa) discountPaisa = subtotalPaisa

  // Pro-rata line discount: floor per line for all but the last, last line
  // absorbs the remainder so the lines sum to discountPaisa exactly. If
  // subtotal is 0 (every line rounded to 0 paisa), every share is 0.
  const lineDiscountsPaisa = new Array<number>(input.lines.length).fill(0)
  if (subtotalPaisa > 0) {
    let allocated = 0
    for (let i = 0; i < input.lines.length - 1; i++) {
      lineDiscountsPaisa[i] = floorDivPaisa(discountPaisa, lineTotalsPaisa[i], subtotalPaisa)
      allocated += lineDiscountsPaisa[i]
    }
    lineDiscountsPaisa[input.lines.length - 1] = discountPaisa - allocated
  }

  const lines: LineOut[] = []
  let taxPaisa = 0
  let taxableSumPaisa = 0
  for (let i = 0; i < input.lines.length; i++) {
    const line = input.lines[i]
    const taxablePaisa = lineTotalsPaisa[i] - lineDiscountsPaisa[i]
    taxableSumPaisa += taxablePaisa
    const lineTaxPaisa = paisa((taxablePaisa / 100) * input.tax_rate)
    taxPaisa += lineTaxPaisa
    lines.push({
      quantity: line.quantity,
      unit_price: line.rate,
      line_total: lineTotalsPaisa[i] / 100,
      line_discount: lineDiscountsPaisa[i] / 100,
      line_taxable: taxablePaisa / 100,
      line_tax: lineTaxPaisa / 100,
    })
  }

  // further_tax = round2(Σ taxable × further_tax_rate), only when the buyer
  // is unregistered (§7.5).
  let furtherTaxPaisa = 0
  if (!input.buyer_registered) {
    furtherTaxPaisa = paisa((taxableSumPaisa / 100) * input.further_tax_rate)
  }

  const totalAmountPaisa = taxableSumPaisa + taxPaisa + furtherTaxPaisa
  const totalPayable = roundHalfUpToRupee(totalAmountPaisa)
  const roundingAdjPaisa = totalPayable * 100 - totalAmountPaisa

  return {
    lines,
    subtotal: subtotalPaisa / 100,
    discount_amount: discountPaisa / 100,
    tax_rate: input.tax_rate,
    tax_amount: taxPaisa / 100,
    further_tax_amount: furtherTaxPaisa / 100,
    total_amount: totalAmountPaisa / 100,
    rounding_adjustment: roundingAdjPaisa / 100,
    total_payable: totalPayable,
  }
}

/** Settings keys taxRateFor reads. A subset of AppSettings so this file
 * does not need the whole shape wired through. */
export type TaxRateSettings = Pick<
  AppSettings,
  'tax_rate_cash' | 'tax_rate_card' | 'tax_rate_online' | 'tax_rate_credit'
>

/** Tax rate for a tender, mirroring the Go TaxRateFor. The frontend already
 * receives settings as parsed numbers (unlike the Go side, which reads raw
 * JSON off the settings table), so this takes the already-parsed shape
 * rather than a raw-JSON map. Tax rate for credit falls back to the cash
 * rate when tax_rate_credit is null (spec §6.3). */
export function taxRateFor(tender: string, settings: TaxRateSettings): number {
  const key = tender.trim().toLowerCase()
  if (key === 'credit') {
    return settings.tax_rate_credit ?? settings.tax_rate_cash
  }
  switch (key) {
    case 'cash':
      return settings.tax_rate_cash
    case 'card':
      return settings.tax_rate_card
    case 'online':
      return settings.tax_rate_online
    default:
      return 0
  }
}
