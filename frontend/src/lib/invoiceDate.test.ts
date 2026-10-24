import { describe, expect, it } from 'vitest'
import {
  DEFAULT_BACKDATE_LIMIT_DAYS,
  MAX_BACKDATE_LIMIT_DAYS,
  backdateLimit,
  checkInvoiceDate,
  earlierTaxMonth,
  invoiceDateBounds,
  invoiceDateMessage,
  isBackDated,
} from './invoiceDate'

// Spec 2026-10-12 §8 + plan 2026-10-24: an admin may date a sale up to
// settings.backdate_limit_days (seeded 40, max 90, 0 = off) before the
// current business date, never after it. The helper mirrors the server's
// invoice_date_future / invoice_date_too_old rules so the till can refuse
// before posting.

describe('invoiceDate (spec 2026-10-12 §8)', () => {
  const today = '2026-10-12'

  it('defaults to 40 and caps at 90, like the server', () => {
    expect(DEFAULT_BACKDATE_LIMIT_DAYS).toBe(40)
    expect(MAX_BACKDATE_LIMIT_DAYS).toBe(90)
    expect(backdateLimit(60)).toBe(60)
    expect(backdateLimit(0)).toBe(0)
    expect(backdateLimit(90)).toBe(90)
    for (const bad of [undefined, null, 91, -1, 40.5]) expect(backdateLimit(bad)).toBe(40)
  })

  it('bounds the picker to the limit and today', () => {
    expect(invoiceDateBounds(today, 40)).toEqual({ min: '2026-09-02', max: '2026-10-12' })
    expect(invoiceDateBounds(today, 60)).toEqual({ min: '2026-08-13', max: '2026-10-12' })
    expect(invoiceDateBounds(today, 90)).toEqual({ min: '2026-07-14', max: '2026-10-12' })
    expect(invoiceDateBounds(today, 0)).toEqual({ min: today, max: today })
  })

  it('crosses a month boundary without a Date-object off-by-one', () => {
    expect(invoiceDateBounds('2026-03-01', 40)).toEqual({ min: '2026-01-20', max: '2026-03-01' })
  })

  it('accepts today and exactly the limit back', () => {
    expect(checkInvoiceDate(today, today, 40)).toBe('ok')
    expect(checkInvoiceDate('2026-09-02', today, 40)).toBe('ok')
    expect(checkInvoiceDate('2026-08-13', today, 60)).toBe('ok')
    expect(checkInvoiceDate(today, today, 0)).toBe('ok')
  })

  it('refuses a future date, one past the limit and garbage', () => {
    expect(checkInvoiceDate('2026-10-13', today, 40)).toBe('future')
    expect(checkInvoiceDate('2026-09-01', today, 40)).toBe('too_old')
    expect(checkInvoiceDate('2026-08-12', today, 60)).toBe('too_old')
    expect(checkInvoiceDate('2026-10-11', today, 0)).toBe('too_old')
    expect(checkInvoiceDate('12/10/2026', today, 40)).toBe('invalid')
    expect(checkInvoiceDate('', today, 40)).toBe('invalid')
  })

  it('says why, in the limit the store chose', () => {
    expect(invoiceDateMessage('too_old', 60)).toBe('A sale can be back-dated at most 60 days.')
    expect(invoiceDateMessage('too_old', 0)).toBe('Back-dating is switched off in Settings.')
    expect(invoiceDateMessage('ok', 60)).toBeNull()
  })

  it('names the earlier tax month a back-date lands in', () => {
    expect(earlierTaxMonth('2026-09-30', today)).toBe('September')
    expect(earlierTaxMonth('2026-10-01', today)).toBeNull()
    expect(earlierTaxMonth(today, today)).toBeNull()
    expect(earlierTaxMonth('2025-12-20', '2026-01-10')).toBe('December 2025')
    expect(earlierTaxMonth('garbage', today)).toBeNull()
  })

  it('isBackDated compares the two dates as bare YYYY-MM-DD strings', () => {
    expect(isBackDated({ invoice_date: '2026-10-05', business_date: '2026-10-12' })).toBe(true)
    expect(isBackDated({ invoice_date: '2026-10-12T00:00:00+05:00', business_date: '2026-10-12T00:00:00+05:00' })).toBe(false)
    expect(isBackDated({ invoice_date: undefined, business_date: '2026-10-12' })).toBe(false)
  })
})
