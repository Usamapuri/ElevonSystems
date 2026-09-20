import { describe, expect, it } from 'vitest'
import {
  cartDiscount,
  cartReducer,
  emptyCart,
  pricingLines,
  requestLines,
  type CartLineDraft,
  type CartState,
} from './cart'

function draft(over: Partial<CartLineDraft> = {}): CartLineDraft {
  return {
    product_id: 'p1',
    product_name: 'LPG bulk',
    rate: 265,
    quantity: 12.5,
    entered_as: 'kg',
    ...over,
  }
}

function withLines(...drafts: CartLineDraft[]): CartState {
  return drafts.reduce((s, line) => cartReducer(s, { type: 'add', line }), emptyCart)
}

describe('cartReducer — add', () => {
  it('appends a line with a fresh key', () => {
    const s = cartReducer(emptyCart, { type: 'add', line: draft() })
    expect(s.lines).toHaveLength(1)
    expect(s.lines[0].key).toBe('line-1')
    expect(s.lines[0].quantity).toBe(12.5)
    expect(s.nextKey).toBe(2)
  })
  it('never merges two lines of the same product', () => {
    const s = withLines(draft(), draft({ quantity: 6 }))
    expect(s.lines.map((l) => l.key)).toEqual(['line-1', 'line-2'])
    expect(s.lines.map((l) => l.quantity)).toEqual([12.5, 6])
  })
  it('does not mutate the previous state', () => {
    const before = withLines(draft())
    const after = cartReducer(before, { type: 'add', line: draft({ quantity: 1 }) })
    expect(before.lines).toHaveLength(1)
    expect(after.lines).toHaveLength(2)
  })
})

describe('cartReducer — update', () => {
  it('replaces the line in place and keeps its key', () => {
    const before = withLines(draft(), draft({ product_id: 'p2', quantity: 3 }))
    const after = cartReducer(before, {
      type: 'update',
      key: 'line-1',
      line: draft({ quantity: 20.125, entered_as: 'tonne' }),
    })
    expect(after.lines[0]).toEqual({
      key: 'line-1',
      product_id: 'p1',
      product_name: 'LPG bulk',
      rate: 265,
      quantity: 20.125,
      entered_as: 'tonne',
    })
    expect(after.lines[1].quantity).toBe(3)
    expect(after.nextKey).toBe(before.nextKey)
  })
  it('is a no-op for an unknown key', () => {
    const before = withLines(draft())
    const after = cartReducer(before, { type: 'update', key: 'line-99', line: draft({ quantity: 99 }) })
    expect(after.lines[0].quantity).toBe(12.5)
  })
  it('drops gross/tare when the line is re-entered in another mode', () => {
    const before = withLines(draft({ entered_as: 'gross_tare', gross_weight: 50.5, tare_weight: 12.25, quantity: 38.25 }))
    const after = cartReducer(before, { type: 'update', key: 'line-1', line: draft({ quantity: 10 }) })
    expect(after.lines[0].gross_weight).toBeUndefined()
    expect(after.lines[0].tare_weight).toBeUndefined()
  })
})

describe('cartReducer — remove and clear', () => {
  it('removes just that line', () => {
    const before = withLines(draft(), draft({ product_id: 'p2' }))
    const after = cartReducer(before, { type: 'remove', key: 'line-1' })
    expect(after.lines).toHaveLength(1)
    expect(after.lines[0].product_id).toBe('p2')
  })
  it('clear empties the lines and the discount but keeps the key counter running', () => {
    let s = withLines(draft(), draft())
    s = cartReducer(s, { type: 'set_discount_value', value: '250' })
    const after = cartReducer(s, { type: 'clear' })
    expect(after.lines).toEqual([])
    expect(after.discountValue).toBe('')
    expect(after.discountMode).toBe('amount')
    expect(after.nextKey).toBe(3)
    expect(cartReducer(after, { type: 'add', line: draft() }).lines[0].key).toBe('line-3')
  })
})

describe('cartReducer — discount', () => {
  it('stores the raw typed value', () => {
    const s = cartReducer(emptyCart, { type: 'set_discount_value', value: '12.5' })
    expect(s.discountValue).toBe('12.5')
  })
  it('clears the figure when the mode flips, so 500 is never reread as 500%', () => {
    let s = cartReducer(emptyCart, { type: 'set_discount_value', value: '500' })
    s = cartReducer(s, { type: 'set_discount_mode', mode: 'percent' })
    expect(s.discountMode).toBe('percent')
    expect(s.discountValue).toBe('')
  })
})

describe('cartDiscount', () => {
  it('reads a blank field as no discount', () => {
    expect(cartDiscount(emptyCart)).toEqual({ discount_amount: 0, discount_percent: null, valid: true })
  })
  it('sends a rupee amount rounded to paisa', () => {
    const s = cartReducer(emptyCart, { type: 'set_discount_value', value: '250.456' })
    expect(cartDiscount(s)).toEqual({ discount_amount: 250.46, discount_percent: null, valid: true })
  })
  it('sends a percent as a percent, with no rupee amount', () => {
    let s = cartReducer(emptyCart, { type: 'set_discount_mode', mode: 'percent' })
    s = cartReducer(s, { type: 'set_discount_value', value: '5' })
    expect(cartDiscount(s)).toEqual({ discount_amount: 0, discount_percent: 5, valid: true })
  })
  it('flags a percent above 100 and any non-number as invalid', () => {
    let pct = cartReducer(emptyCart, { type: 'set_discount_mode', mode: 'percent' })
    pct = cartReducer(pct, { type: 'set_discount_value', value: '120' })
    expect(cartDiscount(pct).valid).toBe(false)

    const junk = cartReducer(emptyCart, { type: 'set_discount_value', value: 'abc' })
    expect(cartDiscount(junk).valid).toBe(false)

    const negative = cartReducer(emptyCart, { type: 'set_discount_value', value: '-5' })
    expect(cartDiscount(negative).valid).toBe(false)
  })
  it('keeps a blank percent field at 0, not null, so the server prices by percent', () => {
    const s = cartReducer(emptyCart, { type: 'set_discount_mode', mode: 'percent' })
    expect(cartDiscount(s).discount_percent).toBe(0)
  })
})

describe('pricingLines / requestLines', () => {
  it('hands pricing exactly the quantities and rates the server will use', () => {
    const s = withLines(draft(), draft({ product_id: 'p2', rate: 310, quantity: 4.125 }))
    expect(pricingLines(s)).toEqual([
      { product_id: 'p1', quantity: 12.5, rate: 265 },
      { product_id: 'p2', quantity: 4.125, rate: 310 },
    ])
  })
  it('sends gross and tare only for a gross_tare line', () => {
    const s = withLines(
      draft({ entered_as: 'gross_tare', gross_weight: 50.5, tare_weight: 12.25, quantity: 38.25 }),
      draft({ entered_as: 'amount', typed_amount: 1000, quantity: 3.774 }),
    )
    expect(requestLines(s)).toEqual([
      { product_id: 'p1', quantity: 38.25, entered_as: 'gross_tare', gross_weight: 50.5, tare_weight: 12.25 },
      { product_id: 'p1', quantity: 3.774, entered_as: 'amount' },
    ])
  })
})
