/**
 * Back-dated invoices (spec 2026-10-12 §8): an admin may date a sale up to
 * BACKDATE_LIMIT_DAYS before the current business date, never after it. The
 * server is the authority (invoice_date_requires_admin / _future / _too_old);
 * this mirror lets the till bound the calendar and refuse before posting.
 *
 * Dates are bare `YYYY-MM-DD` strings throughout, compared as strings — the
 * format sorts lexically — so no Date object ever converts a business date
 * through the browser's timezone (see lib/print/format.ts rule 2).
 */

export const BACKDATE_LIMIT_DAYS = 30

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

/** Adds `days` (may be negative) to a `YYYY-MM-DD` key in UTC arithmetic,
 * which is timezone-free because the key never carries a time. */
function shiftDateKey(key: string, days: number): string {
  const [y, m, d] = key.split('-').map(Number)
  const t = Date.UTC(y, m - 1, d + days)
  return new Date(t).toISOString().slice(0, 10)
}

/** The calendar's `min`/`max` for a given business date. */
export function invoiceDateBounds(todayKey: string): { min: string; max: string } {
  return { min: shiftDateKey(todayKey, -BACKDATE_LIMIT_DAYS), max: todayKey }
}

export type InvoiceDateCheck = 'ok' | 'invalid' | 'future' | 'too_old'

export function checkInvoiceDate(value: string, todayKey: string): InvoiceDateCheck {
  if (!DATE_RE.test(value)) return 'invalid'
  if (value > todayKey) return 'future'
  if (value < invoiceDateBounds(todayKey).min) return 'too_old'
  return 'ok'
}

export function invoiceDateMessage(check: InvoiceDateCheck): string | null {
  switch (check) {
    case 'future':
      return 'The sale date cannot be after today.'
    case 'too_old':
      return `A sale can be back-dated at most ${BACKDATE_LIMIT_DAYS} days.`
    case 'invalid':
      return 'Pick a valid sale date.'
    default:
      return null
  }
}

/** True when the document date differs from the day the sale was booked on.
 * Both arrive as `YYYY-MM-DD` or an ISO timestamp at local midnight; only
 * the first ten characters are the date. */
export function isBackDated(inv: { invoice_date?: string | null; business_date: string }): boolean {
  if (!inv.invoice_date) return false
  return inv.invoice_date.slice(0, 10) !== inv.business_date.slice(0, 10)
}
