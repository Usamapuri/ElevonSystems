/**
 * Back-dated invoices (spec 2026-10-12 §8): an admin may date a sale up to
 * `settings.backdate_limit_days` before the current business date, never
 * after it. The server is the authority (invoice_date_requires_admin /
 * _future / _too_old) and reads the same setting; this mirror lets the till
 * bound the calendar and refuse before posting.
 *
 * Dates are bare `YYYY-MM-DD` strings throughout, compared as strings — the
 * format sorts lexically — so no Date object ever converts a business date
 * through the browser's timezone (see lib/print/format.ts rule 2).
 */

/** Seeded value of `backdate_limit_days` (migration 008), and what the till
 * assumes before settings load. The server caps the setting at 90. */
export const DEFAULT_BACKDATE_LIMIT_DAYS = 40
export const MAX_BACKDATE_LIMIT_DAYS = 90

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

/** Adds `days` (may be negative) to a `YYYY-MM-DD` key in UTC arithmetic,
 * which is timezone-free because the key never carries a time. */
function shiftDateKey(key: string, days: number): string {
  const [y, m, d] = key.split('-').map(Number)
  const t = Date.UTC(y, m - 1, d + days)
  return new Date(t).toISOString().slice(0, 10)
}

/** The setting as the till uses it: a whole number of days, 0 = off. */
export function backdateLimit(setting: number | undefined | null): number {
  if (typeof setting !== 'number' || !Number.isInteger(setting) || setting < 0 || setting > MAX_BACKDATE_LIMIT_DAYS) {
    return DEFAULT_BACKDATE_LIMIT_DAYS
  }
  return setting
}

/** The calendar's `min`/`max` for a given business date. */
export function invoiceDateBounds(todayKey: string, limitDays: number): { min: string; max: string } {
  return { min: shiftDateKey(todayKey, -limitDays), max: todayKey }
}

export type InvoiceDateCheck = 'ok' | 'invalid' | 'future' | 'too_old'

export function checkInvoiceDate(value: string, todayKey: string, limitDays: number): InvoiceDateCheck {
  if (!DATE_RE.test(value)) return 'invalid'
  if (value > todayKey) return 'future'
  if (value < invoiceDateBounds(todayKey, limitDays).min) return 'too_old'
  return 'ok'
}

export function invoiceDateMessage(check: InvoiceDateCheck, limitDays: number): string | null {
  switch (check) {
    case 'future':
      return 'The sale date cannot be after today.'
    case 'too_old':
      return limitDays <= 0 ? 'Back-dating is switched off in Settings.' : `A sale can be back-dated at most ${limitDays} days.`
    case 'invalid':
      return 'Pick a valid sale date.'
    default:
      return null
  }
}

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

/** The month name when a back-date falls in an earlier calendar month than
 * the business date — the sale then belongs to a tax period whose return may
 * already be filed. Null for same-month dates and anything unparseable. */
export function earlierTaxMonth(value: string, todayKey: string): string | null {
  if (!DATE_RE.test(value) || !DATE_RE.test(todayKey)) return null
  if (value.slice(0, 7) >= todayKey.slice(0, 7)) return null
  const month = MONTHS[Number(value.slice(5, 7)) - 1]
  return value.slice(0, 4) === todayKey.slice(0, 4) ? month : `${month} ${value.slice(0, 4)}`
}

/** True when the document date differs from the day the sale was booked on.
 * Both arrive as `YYYY-MM-DD` or an ISO timestamp at local midnight; only
 * the first ten characters are the date. */
export function isBackDated(inv: { invoice_date?: string | null; business_date: string }): boolean {
  if (!inv.invoice_date) return false
  return inv.invoice_date.slice(0, 10) !== inv.business_date.slice(0, 10)
}
