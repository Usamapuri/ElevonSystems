/**
 * Whether the till may sell right now, mirrored from the server's four
 * branches (dayops.EnsureOpenDayForInvoice, spec §6.7):
 *
 *   1. today's day is open          → sell
 *   2. some OTHER date is open      → 409 previous_day_open, close it first
 *   3. today's day is closed        → sell; the sale reopens it, audited
 *   4. today has never been opened  → 409 day_not_open
 *
 * This is a display decision only. The server re-decides inside the invoice
 * transaction and is the authority; all this does is stop the cashier from
 * ringing a whole sale into a 409, and say which screen fixes it.
 */
import type { BusinessDay } from '@/types'

export type DayGate =
  | { kind: 'loading' }
  | { kind: 'ok' }
  /** Today is sealed. A sale still goes through and reopens it, with an
   * audit row — worth saying out loud, not worth blocking. */
  | { kind: 'late_sale'; date: string }
  | { kind: 'day_not_open' }
  | { kind: 'previous_day_open'; date: string }

/**
 * The business date as util.BusinessDate computes it: shift back by the
 * boundary hour, then take the calendar date.
 *
 * Pinned to Asia/Karachi via Intl rather than the browser's own clock and
 * zone: this feeds the invoice/reports date presets (components/invoices
 * /dateRange.ts) as well as the till's own day gate, and an owner checking
 * reports from outside the shop — or a demo machine whose zone was never
 * set — must land on the same "today" the server does, not a banner that
 * merely "gets it wrong" for the till alone.
 */
const businessDateFormatter = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Karachi',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

export function businessDateKey(now: Date, boundaryHour: number): string {
  const h = Number.isFinite(boundaryHour) ? Math.min(Math.max(Math.trunc(boundaryHour), 0), 12) : 0
  const shifted = new Date(now.getTime() - h * 3_600_000)
  // en-CA formats as YYYY-MM-DD, which is what every caller here expects.
  return businessDateFormatter.format(shifted)
}

/** The day's own date, read off the wire string rather than through Date —
 * business_date arrives already in the business timezone and parsing it
 * would only give a browser in another zone a chance to shift it. */
export function dayDateKey(day: BusinessDay): string {
  return day.business_date.slice(0, 10)
}

export function evaluateDayGate(day: BusinessDay | null | undefined, boundaryHour: number, now: Date): DayGate {
  if (!day) return { kind: 'day_not_open' }
  const today = businessDateKey(now, boundaryHour)
  const key = dayDateKey(day)
  if (day.status === 'open' || day.status === 'reopened') {
    return key === today ? { kind: 'ok' } : { kind: 'previous_day_open', date: key }
  }
  return key === today ? { kind: 'late_sale', date: key } : { kind: 'day_not_open' }
}

/** True when no sale can be rung. A late sale is not blocked (branch 3). */
export function gateBlocks(gate: DayGate): boolean {
  return gate.kind === 'loading' || gate.kind === 'day_not_open' || gate.kind === 'previous_day_open'
}

/** Maps the two server codes a charge can come back with onto the gate, so
 * a 409 the banner did not predict still lands the cashier in the right
 * place (a day closed in another tab, say). */
export function gateFromErrorCode(code: string | undefined): DayGate | null {
  if (code === 'day_not_open') return { kind: 'day_not_open' }
  if (code === 'previous_day_open') return { kind: 'previous_day_open', date: '' }
  return null
}
