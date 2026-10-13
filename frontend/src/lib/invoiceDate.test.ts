import { describe, expect, it } from 'vitest'
import { BACKDATE_LIMIT_DAYS, checkInvoiceDate, invoiceDateBounds, isBackDated } from './invoiceDate'

// Spec 2026-10-12 §8: an admin may date a sale up to 40 days before the
// current business date, never after it. The helper mirrors the server's
// invoice_date_future / invoice_date_too_old rules so the till can refuse
// before posting.

describe('invoiceDate (spec 2026-10-12 §8)', () => {
  const today = '2026-10-12'

  it('bounds the picker to 40 days back and today', () => {
    expect(BACKDATE_LIMIT_DAYS).toBe(40)
    expect(invoiceDateBounds(today)).toEqual({ min: '2026-09-02', max: '2026-10-12' })
  })

  it('crosses a month boundary without a Date-object off-by-one', () => {
    expect(invoiceDateBounds('2026-03-01')).toEqual({ min: '2026-01-20', max: '2026-03-01' })
  })

  it('accepts today and exactly 40 days back', () => {
    expect(checkInvoiceDate(today, today)).toBe('ok')
    expect(checkInvoiceDate('2026-09-02', today)).toBe('ok')
  })

  it('refuses a future date, a date older than 40 days and garbage', () => {
    expect(checkInvoiceDate('2026-10-13', today)).toBe('future')
    expect(checkInvoiceDate('2026-09-01', today)).toBe('too_old')
    expect(checkInvoiceDate('12/10/2026', today)).toBe('invalid')
    expect(checkInvoiceDate('', today)).toBe('invalid')
  })

  it('isBackDated compares the two dates as bare YYYY-MM-DD strings', () => {
    expect(isBackDated({ invoice_date: '2026-10-05', business_date: '2026-10-12' })).toBe(true)
    expect(isBackDated({ invoice_date: '2026-10-12T00:00:00+05:00', business_date: '2026-10-12T00:00:00+05:00' })).toBe(false)
    expect(isBackDated({ invoice_date: undefined, business_date: '2026-10-12' })).toBe(false)
  })
})
