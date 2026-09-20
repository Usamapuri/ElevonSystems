/**
 * The invoice browser's date presets.
 *
 * `GET /invoices` filters on `business_date`, which is a bare `YYYY-MM-DD`
 * the server resolved in Asia/Karachi against `day_boundary_hour`. Every key
 * here is computed and compared as that string: pushing a business date
 * through `new Date()` parses it as UTC midnight and renders it back in the
 * viewer's zone, which is how "today" silently becomes yesterday.
 *
 * `businessDateKey` (components/pos/dayGate.ts) already owns the one
 * conversion that has to touch a clock — now to today's business date — so it
 * is reused rather than reimplemented; everything below stays in the date
 * domain.
 */
import { businessDateKey } from '@/components/pos/dayGate'

export type DatePreset = 'today' | 'yesterday' | 'last7' | 'custom'

export interface DateRange {
  from: string
  to: string
}

export function presetLabel(preset: DatePreset): string {
  switch (preset) {
    case 'today':
      return 'Today'
    case 'yesterday':
      return 'Yesterday'
    case 'last7':
      return 'Last 7 days'
    case 'custom':
      return 'Custom'
  }
}

/** The presets offered as buttons, in order. `custom` is what the two date
 * inputs select and is never a button. */
export const DATE_PRESETS: readonly DatePreset[] = ['today', 'yesterday', 'last7'] as const

export function isDateKey(value: string | null | undefined): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(value ?? '')
}

/**
 * Shifts a `YYYY-MM-DD` key by whole days. `Date.UTC` is arithmetic on a
 * fixed offset, so it cannot drift across a zone or a DST edge the way a
 * local-time Date can; the result is formatted straight back out of the UTC
 * getters and never rendered through a locale.
 */
export function shiftDateKey(key: string, days: number): string {
  if (!isDateKey(key)) return key
  const [y, m, d] = key.split('-').map(Number)
  const t = new Date(Date.UTC(y, m - 1, d))
  t.setUTCDate(t.getUTCDate() + days)
  const yy = t.getUTCFullYear()
  const mm = String(t.getUTCMonth() + 1).padStart(2, '0')
  const dd = String(t.getUTCDate()).padStart(2, '0')
  return `${yy}-${mm}-${dd}`
}

/**
 * The inclusive `from`/`to` for a preset, against today's business date.
 * `last7` is the last seven business days *including* today, which is what
 * an owner means by "the last week" when they are standing at the till.
 * `custom` has no computable range — the caller keeps whatever is typed.
 */
export function presetRange(preset: DatePreset, now: Date, boundaryHour: number): DateRange | null {
  if (preset === 'custom') return null
  const today = businessDateKey(now, boundaryHour)
  switch (preset) {
    case 'today':
      return { from: today, to: today }
    case 'yesterday': {
      const y = shiftDateKey(today, -1)
      return { from: y, to: y }
    }
    case 'last7':
      return { from: shiftDateKey(today, -6), to: today }
  }
}

/** Which preset a range corresponds to, so the buttons stay lit after a
 * reload or a hand-typed date that happens to match one. */
export function matchPreset(range: DateRange, now: Date, boundaryHour: number): DatePreset {
  for (const preset of DATE_PRESETS) {
    const candidate = presetRange(preset, now, boundaryHour)
    if (candidate && candidate.from === range.from && candidate.to === range.to) return preset
  }
  return 'custom'
}

/** True when the range is the wrong way round — the server would answer an
 * empty page and the person would read it as "no sales". */
export function rangeInverted(range: DateRange): boolean {
  if (!isDateKey(range.from) || !isDateKey(range.to)) return false
  return range.from > range.to
}

/** Drops the blank ends so a half-filled custom range still filters on the
 * end that was typed, rather than sending an empty `from` to the server. */
export function rangeParams(range: DateRange): { from?: string; to?: string } {
  return {
    ...(isDateKey(range.from) ? { from: range.from } : {}),
    ...(isDateKey(range.to) ? { to: range.to } : {}),
  }
}
