import { describe, expect, it } from 'vitest'
import { businessDateKey, dayDateKey, evaluateDayGate, gateBlocks, gateFromErrorCode } from './dayGate'
import type { BusinessDay } from '@/types'

function day(over: Partial<BusinessDay>): BusinessDay {
  return {
    id: 'd1',
    business_date: '2026-09-20T00:00:00+05:00',
    status: 'open',
    opened_at: '2026-09-20T08:00:00+05:00',
    opened_by: null,
    opened_by_name: null,
    opening_cash: 0,
    opening_notes: null,
    closed_at: null,
    closed_by: null,
    closed_by_name: null,
    counted_cash: null,
    counted_card: null,
    counted_online: null,
    expected_cash: null,
    expected_card: null,
    expected_online: null,
    cash_variance: null,
    card_variance: null,
    online_variance: null,
    gross_sales: null,
    discounts: null,
    tax_collected: null,
    net_sales: null,
    on_account_sales: null,
    receipts_collected: null,
    invoice_count: null,
    void_count: null,
    closing_notes: null,
    created_at: '2026-09-20T08:00:00+05:00',
    updated_at: '2026-09-20T08:00:00+05:00',
    ...over,
  }
}

/** Local-time noon, so the boundary-hour shift never crosses a date by
 * accident on the machine running the suite. */
const noon = new Date(2026, 8, 20, 12, 0, 0)

describe('businessDateKey', () => {
  it('is the calendar date at a zero boundary hour', () => {
    expect(businessDateKey(noon, 0)).toBe('2026-09-20')
  })
  it('keeps the previous date before the boundary hour', () => {
    expect(businessDateKey(new Date(2026, 8, 20, 3, 0, 0), 6)).toBe('2026-09-19')
    expect(businessDateKey(new Date(2026, 8, 20, 7, 0, 0), 6)).toBe('2026-09-20')
  })
  it('clamps a nonsense boundary hour rather than shifting a report', () => {
    expect(businessDateKey(noon, -5)).toBe('2026-09-20')
    expect(businessDateKey(noon, 99)).toBe('2026-09-20')
    expect(businessDateKey(noon, NaN)).toBe('2026-09-20')
  })
})

describe('dayDateKey', () => {
  it('reads the date off the wire string without parsing it', () => {
    expect(dayDateKey(day({ business_date: '2026-09-19T00:00:00+05:00' }))).toBe('2026-09-19')
  })
})

describe('evaluateDayGate', () => {
  it('branch 1 — today is open', () => {
    expect(evaluateDayGate(day({ status: 'open' }), 0, noon)).toEqual({ kind: 'ok' })
    expect(evaluateDayGate(day({ status: 'reopened' }), 0, noon)).toEqual({ kind: 'ok' })
  })
  it('branch 2 — another date holds the open slot', () => {
    expect(evaluateDayGate(day({ business_date: '2026-09-19T00:00:00+05:00' }), 0, noon)).toEqual({
      kind: 'previous_day_open',
      date: '2026-09-19',
    })
  })
  it('branch 3 — today is closed: a late sale reopens it, so it is not blocked', () => {
    const gate = evaluateDayGate(day({ status: 'closed' }), 0, noon)
    expect(gate).toEqual({ kind: 'late_sale', date: '2026-09-20' })
    expect(gateBlocks(gate)).toBe(false)
  })
  it('branch 4 — no day at all, or only a stale closed one', () => {
    expect(evaluateDayGate(null, 0, noon)).toEqual({ kind: 'day_not_open' })
    expect(evaluateDayGate(day({ status: 'closed', business_date: '2026-09-19T00:00:00+05:00' }), 0, noon)).toEqual({
      kind: 'day_not_open',
    })
  })
})

describe('gateBlocks', () => {
  it('blocks until the day is known, and on both 409 cases', () => {
    expect(gateBlocks({ kind: 'loading' })).toBe(true)
    expect(gateBlocks({ kind: 'day_not_open' })).toBe(true)
    expect(gateBlocks({ kind: 'previous_day_open', date: '2026-09-19' })).toBe(true)
    expect(gateBlocks({ kind: 'ok' })).toBe(false)
  })
})

describe('gateFromErrorCode', () => {
  it('maps the two day codes and nothing else', () => {
    expect(gateFromErrorCode('day_not_open')).toEqual({ kind: 'day_not_open' })
    expect(gateFromErrorCode('previous_day_open')).toEqual({ kind: 'previous_day_open', date: '' })
    expect(gateFromErrorCode('credit_limit_exceeded')).toBeNull()
    expect(gateFromErrorCode(undefined)).toBeNull()
  })
})
