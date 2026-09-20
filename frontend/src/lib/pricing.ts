// The one money function (spec §6.3), mirrored from the Go source of truth
// (backend/internal/pricing/pricing.go) for the live checkout preview only.
// The server recomputes every invoice from products.rate; this file must
// never be trusted for the figure that actually gets charged. Both suites
// load the same JSON fixture (pricing_fixture.json / backend/internal/
// pricing/testdata/pricing_fixture.json) and a parity test in each pins the
// two files byte identical.
//
// Every amount that could land on an exact half-paisa (or half-rupee) tie
// is computed via scaled BigInt arithmetic and halfUpDivBig, never by
// rounding a plain float64 multiplication result directly — this is the
// same algorithm as the Go side, using BigInt in place of int64 wherever a
// product of two paisa-scale integers could exceed 2^53. See halfUpDivBig's
// doc comment for why a naive float approach silently mis-rounds real
// inputs (e.g. 1.25×0.18 and 0.009×265, both exact ties).
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
 * paisa-exact rupees. rounding_adjustment's achievable range under
 * half-up-ties-up is −0.49 … +0.50 (paisa fraction 49 rounds down to −0.49,
 * paisa fraction 50 rounds up to +0.50) — the design spec and the original
 * task brief both say "−0.50 … +0.49", which is off by a cent at both
 * ends; see the fixture cases `tie_rounds_up_line_total` (−0.39) and
 * `rounding_half_up` (+0.50) for the achieved bounds in practice. */
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
  constructor(public code: 'no_lines' | 'invalid_quantity' | 'invalid_rate' | 'invalid_discount' | 'invalid_tax_rate') {
    super(code)
    this.name = 'PricingError'
  }
}

/** Rupee float → integer paisa (port of the Go source's Paisa, itself a
 * port of the retail POS's moneyPaisa): a single float multiplication
 * rounded with Math.round. Kept exactly as the original brief specified it;
 * computeTotals no longer calls it for any value that could land on an
 * exact tie (see halfUpDivBig) — but a raw float that happens to be an
 * exact x.xx5 tie can still be misrounded by a single ULP of float
 * multiplication error, the same class of bug fixed in round2 below. Prefer
 * round2 for any float that might be an exact 2dp tie. */
export function paisa(v: number): number {
  return Math.round(v * 100)
}

/** Rounds a number to 2dp, half-up, robust to the float multiplication
 * error that can flip an exact tie the wrong way. Naively rounding at the
 * target scale (Math.round(v*100)) fails on inputs like 1.25*0.18: the
 * mathematically exact product is 0.225 (an exact half-paisa tie), but the
 * float64 result of that multiplication is 0.22499999999999998 (one ULP
 * low), so v*100 evaluates to 22.499999999999996 and rounds DOWN to 22
 * (0.22) instead of up to 23 (0.23).
 *
 * The fix scales to a much finer grid first (v*1e6, micro-rupees) before
 * rounding to an integer: the same ~1e-15 relative float error is now
 * utterly negligible next to the 0.5 threshold at THIS scale (225000 vs.
 * the computed 224999.99999999997 — off by 3e-11, nowhere near a tie), so
 * Math.round(v*1e6) reliably recovers the true integer value. Any genuine
 * tie at the 2dp/paisa level is then resolved by an exact BigInt division
 * (halfUpDivBig, no floats involved) rather than by float rounding.
 *
 * Callers only ever pass non-negative money into this function (discounts
 * are clamped ≥ 0 elsewhere in this file); the sign handling is defensive. */
export function round2(v: number): number {
  const microRupees = Math.round(v * 1e6)
  const neg = microRupees < 0
  const paisaVal = Number(halfUpDivBig(BigInt(Math.abs(microRupees)), 10000n))
  return (neg ? -paisaVal : paisaVal) / 100
}

/** Divides two non-negative BigInts, rounding the quotient half up:
 * (n + d/2n) / d. d is always one of this file's fixed scale factors
 * (10000n, 1_000_000n, 100_000_000n), all even, so d/2n is exact and this
 * never touches a float. This is the one place a genuine tie (the
 * mathematically exact result lands precisely halfway between two integers
 * at the target scale) gets resolved — deterministically, the same way in
 * Go and TS, independent of any float representation question. BigInt also
 * sidesteps the > 2^53 precision loss a plain Number multiplication of two
 * paisa-scale integers could hit on a large invoice; Go's int64 gets that
 * for free. */
function halfUpDivBig(n: bigint, d: bigint): bigint {
  return (n + d / 2n) / d
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

/** Rejects NaN, ±Infinity and negative values — the shared check for tax
 * rates and discount amount/percent ('invalid_tax_rate' / 'invalid_discount').
 * Zero is allowed (a 0% tax rate or a 0 discount are both ordinary, valid
 * inputs). */
function validNonNegative(v: number): boolean {
  return Number.isFinite(v) && v >= 0
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

/** Implements spec §6.3. Every amount that could land on an exact tie is
 * computed via scaled BigInt arithmetic (see halfUpDivBig), so the result
 * never depends on float64 multiplication rounding the way a direct
 * paisa(qty*rate)-style computation can (see round2's doc comment for a
 * worked example of that failure mode). The three scale factors are:
 * quantity ×10000 (q), rate ×100 (r), and tax/further-tax/discount-percent
 * ×1e6 (t, f, p) — chosen so every product below lands on a whole multiple
 * of the target unit's reciprocal, keeping halfUpDivBig's tie-break exact. */
export function computeTotals(input: Input): Totals {
  if (input.lines.length === 0) {
    throw new PricingError('no_lines')
  }
  if (!validNonNegative(input.tax_rate) || !validNonNegative(input.further_tax_rate)) {
    throw new PricingError('invalid_tax_rate')
  }
  const hasPercent = input.discount_percent !== null && input.discount_percent !== undefined
  if (hasPercent) {
    if (!validNonNegative(input.discount_percent as number)) throw new PricingError('invalid_discount')
  } else if (!validNonNegative(input.discount_amount)) {
    throw new PricingError('invalid_discount')
  }

  const n = input.lines.length
  const lineTotalsPaisa: bigint[] = []
  let subtotalPaisa = 0n
  for (const line of input.lines) {
    if (!validQuantity(line.quantity)) {
      throw new PricingError('invalid_quantity')
    }
    if (!validRate(line.rate)) {
      throw new PricingError('invalid_rate')
    }
    const q = BigInt(Math.round(line.quantity * 10000))
    const r = BigInt(Math.round(line.rate * 100))
    // q*r = qty×rate×1e6 (micro-rupees); /10000 collapses that to paisa
    // with an exact half-up tie-break — no float multiplication of qty by
    // rate ever happens.
    const lt = halfUpDivBig(q * r, 10000n)
    lineTotalsPaisa.push(lt)
    subtotalPaisa += lt
  }

  const t = BigInt(Math.round(input.tax_rate * 1e6))
  const f = BigInt(Math.round(input.further_tax_rate * 1e6))

  // discount = pct ? min(halfUpDiv(subtotal×p, 1e8), subtotal)
  //               : min(round(amount×100), subtotal), both clamped ≥ 0.
  let rawDiscountPaisa: bigint
  if (hasPercent) {
    const p = BigInt(Math.round((input.discount_percent as number) * 1e6))
    // subtotal×p = subtotalPaisa × pct × 1e6; /1e8 = ×pct/100, i.e. the
    // pct-of-subtotal discount in paisa, half-up.
    rawDiscountPaisa = halfUpDivBig(subtotalPaisa * p, 100_000_000n)
  } else {
    rawDiscountPaisa = BigInt(Math.round(input.discount_amount * 100))
  }
  let discountPaisa = rawDiscountPaisa
  if (discountPaisa < 0n) discountPaisa = 0n
  if (discountPaisa > subtotalPaisa) discountPaisa = subtotalPaisa

  // Pro-rata line discount via the largest-remainder method: floor each
  // line's exact share, then hand the leftover paisa (discount − Σfloors,
  // always < n) one at a time to the lines with the largest fractional
  // remainder — never to a fixed "last line", which can push that line's
  // discount past its own line_total and make line_taxable negative (a
  // real failure mode with enough lines and a steep enough discount). Every
  // remainder here shares the same denominator (subtotalPaisa), so
  // comparing the numerators directly orders them correctly — no floats.
  // If subtotal is 0 (every line rounded to 0 paisa), every share is 0.
  const lineDiscountsPaisa: bigint[] = new Array(n).fill(0n)
  if (subtotalPaisa > 0n) {
    const remainders: bigint[] = new Array(n).fill(0n)
    let floorSum = 0n
    for (let i = 0; i < n; i++) {
      const prod = discountPaisa * lineTotalsPaisa[i]
      lineDiscountsPaisa[i] = prod / subtotalPaisa
      remainders[i] = prod % subtotalPaisa
      floorSum += lineDiscountsPaisa[i]
    }
    let deficit = discountPaisa - floorSum

    const order = Array.from({ length: n }, (_, i) => i)
    // Stable sort by remainder descending — ties keep original line order,
    // identically to Go's sort.SliceStable, so the two mirrors always agree.
    order.sort((a, b) => {
      const diff = remainders[b] - remainders[a]
      if (diff > 0n) return 1
      if (diff < 0n) return -1
      return 0
    })

    // One pass normally exhausts the deficit (it is always < n given
    // discount ≤ subtotal); a second pass is defensive so the "never
    // exceed a line's own total" cap can never leave paisa undistributed.
    for (let pass = 0; pass < 2 && deficit > 0n; pass++) {
      for (const idx of order) {
        if (deficit <= 0n) break
        if (lineDiscountsPaisa[idx] < lineTotalsPaisa[idx]) {
          lineDiscountsPaisa[idx] += 1n
          deficit -= 1n
        }
      }
    }
  }

  const lines: LineOut[] = []
  let taxPaisa = 0n
  let taxableSumPaisa = 0n
  for (let i = 0; i < n; i++) {
    const line = input.lines[i]
    const taxablePaisa = lineTotalsPaisa[i] - lineDiscountsPaisa[i] // ≥ 0 by construction
    taxableSumPaisa += taxablePaisa
    const lineTaxPaisa = halfUpDivBig(taxablePaisa * t, 1_000_000n)
    taxPaisa += lineTaxPaisa
    lines.push({
      quantity: line.quantity,
      unit_price: line.rate,
      line_total: Number(lineTotalsPaisa[i]) / 100,
      line_discount: Number(lineDiscountsPaisa[i]) / 100,
      line_taxable: Number(taxablePaisa) / 100,
      line_tax: Number(lineTaxPaisa) / 100,
    })
  }

  // further_tax = halfUpDiv(Σ taxable × further_tax_rate), only when the
  // buyer is unregistered (§7.5).
  let furtherTaxPaisa = 0n
  if (!input.buyer_registered) {
    furtherTaxPaisa = halfUpDivBig(taxableSumPaisa * f, 1_000_000n)
  }

  const totalAmountPaisa = Number(taxableSumPaisa + taxPaisa + furtherTaxPaisa)
  const totalPayable = roundHalfUpToRupee(totalAmountPaisa)
  const roundingAdjPaisa = totalPayable * 100 - totalAmountPaisa

  return {
    lines,
    subtotal: Number(subtotalPaisa) / 100,
    discount_amount: Number(discountPaisa) / 100,
    tax_rate: input.tax_rate,
    tax_amount: Number(taxPaisa) / 100,
    further_tax_amount: Number(furtherTaxPaisa) / 100,
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
 * rate when tax_rate_credit is null (spec §6.3). A tender that is not
 * cash/card/online/credit returns 0, not the cash rate — fail closed
 * rather than silently charging a rate the caller never configured for
 * that tender. */
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
