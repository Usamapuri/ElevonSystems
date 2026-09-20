/**
 * The till's cart: lines and the invoice discount, and nothing else.
 *
 * It is a plain reducer with no React in it so it can be unit-tested and so
 * the screen has exactly one place that decides what "the cart" is. The
 * customer and the tender live in the page, not here — they are settlement
 * questions, and the cart survives a cancelled tender dialog untouched.
 *
 * No money is stored on a line beyond the product's rate snapshot: the
 * preview is derived through lib/pricing.computeTotals and the figure that
 * is actually charged is recomputed by the server from products.rate.
 */
import type { EnteredAs, InvoiceLineRequest } from '@/types'
import { round2, type LineIn } from '@/lib/pricing'
import { round3 } from '@/lib/weight'

export interface CartLine {
  /** Stable within one cart, so React keys and edits survive a reorder. */
  key: string
  product_id: string
  product_name: string
  /** products.rate as it was when the line was rung. The server re-reads the
   * live rate at charge time; a mid-sale rate change moves the total, which
   * is correct and is why the preview is never the authority. */
  rate: number
  /** Net kg, 3 dp, whatever mode produced it. */
  quantity: number
  entered_as: EnteredAs
  gross_weight?: number
  tare_weight?: number
  /** The rupee figure the customer actually named in amount mode. Display
   * only — it is not sent and is not the line total, which is recomputed
   * from the rounded quantity and can differ by a paisa or two. */
  typed_amount?: number
}

/** A line as the pad produces it, before the cart gives it a key. */
export type CartLineDraft = Omit<CartLine, 'key'>

export type DiscountMode = 'amount' | 'percent'

export interface CartState {
  lines: CartLine[]
  discountMode: DiscountMode
  /** Raw input text: "" means no discount, not zero-and-invalid. */
  discountValue: string
  /** Monotonic key source. Deterministic so the reducer is testable. */
  nextKey: number
}

export type CartAction =
  | { type: 'add'; line: CartLineDraft }
  | { type: 'update'; key: string; line: CartLineDraft }
  | { type: 'remove'; key: string }
  | { type: 'set_discount_mode'; mode: DiscountMode }
  | { type: 'set_discount_value'; value: string }
  | { type: 'clear' }

export const emptyCart: CartState = {
  lines: [],
  discountMode: 'amount',
  discountValue: '',
  nextKey: 1,
}

export function cartReducer(state: CartState, action: CartAction): CartState {
  switch (action.type) {
    case 'add':
      // A tile tap always makes a new line: two cylinders of the same
      // product at two weights are two lines on the receipt, not one merged
      // quantity nobody weighed.
      return {
        ...state,
        lines: [...state.lines, { ...action.line, key: `line-${state.nextKey}` }],
        nextKey: state.nextKey + 1,
      }
    case 'update':
      return {
        ...state,
        lines: state.lines.map((l) => (l.key === action.key ? { ...action.line, key: l.key } : l)),
      }
    case 'remove':
      return { ...state, lines: state.lines.filter((l) => l.key !== action.key) }
    case 'set_discount_mode':
      // Switching amount ↔ percent clears the figure: "500" means something
      // very different in the other mode and silently reinterpreting it is
      // a customer-visible money bug waiting to happen.
      return { ...state, discountMode: action.mode, discountValue: '' }
    case 'set_discount_value':
      return { ...state, discountValue: action.value }
    case 'clear':
      // The key counter keeps running so a cleared-and-refilled cart never
      // reuses a key a pending render still holds.
      return { ...emptyCart, nextKey: state.nextKey }
  }
}

export interface CartDiscount {
  discount_amount: number
  discount_percent: number | null
  /** False when the typed figure is not a number the server would accept. */
  valid: boolean
}

/**
 * The discount as both pricing and POST /invoices want it: a percent when
 * one was typed (discount_amount is then ignored by the server), a rupee
 * amount otherwise. A blank field is no discount, which is always valid.
 */
export function cartDiscount(state: CartState): CartDiscount {
  const raw = state.discountValue.trim()
  if (raw === '') {
    return { discount_amount: 0, discount_percent: state.discountMode === 'percent' ? 0 : null, valid: true }
  }
  const n = Number(raw)
  if (!Number.isFinite(n) || n < 0) {
    return { discount_amount: 0, discount_percent: state.discountMode === 'percent' ? 0 : null, valid: false }
  }
  if (state.discountMode === 'percent') {
    if (n > 100) return { discount_amount: 0, discount_percent: 0, valid: false }
    return { discount_amount: 0, discount_percent: n, valid: true }
  }
  return { discount_amount: round2(n), discount_percent: null, valid: true }
}

/** Cart lines as lib/pricing.computeTotals wants them — the same quantities
 * and rates the server will price from. */
export function pricingLines(state: CartState): LineIn[] {
  return state.lines.map((l) => ({ product_id: l.product_id, quantity: l.quantity, rate: l.rate }))
}

/** Cart lines as POST /invoices wants them. gross/tare travel only for the
 * mode that produced them, matching the server's parse. */
export function requestLines(state: CartState): InvoiceLineRequest[] {
  return state.lines.map((l) => {
    const line: InvoiceLineRequest = {
      product_id: l.product_id,
      quantity: round3(l.quantity),
      entered_as: l.entered_as,
    }
    if (l.entered_as === 'gross_tare' && l.gross_weight !== undefined && l.tare_weight !== undefined) {
      line.gross_weight = round3(l.gross_weight)
      line.tare_weight = round3(l.tare_weight)
    }
    return line
  })
}
