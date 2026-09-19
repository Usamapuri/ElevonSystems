import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'
import { format as formatDateFns, parse as parseDateFns, isValid as isValidDateFns } from 'date-fns'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}


// ---------------------------------------------------------------------------
// Date formatting — DD-MM-YYYY everywhere a human reads a date.
//
// The user has mandated DD-MM-YYYY across the entire UI. To make this safe
// and uniform, every date helper here renders day-month-year. ISO YYYY-MM-DD
// is reserved for the wire (API params, URL state, JSON) where a sortable,
// unambiguous format is necessary; use `toIsoDate(...)` for that case.
// ---------------------------------------------------------------------------

function toDate(input: Date | string | number | null | undefined): Date | null {
  if (input == null) return null
  if (input instanceof Date) return isValidDateFns(input) ? input : null
  const d = new Date(input)
  return isValidDateFns(d) ? d : null
}

/** DD-MM-YYYY (e.g. "18-04-2026"). Returns "—" for invalid input. */
export function formatDateDDMMYYYY(input: Date | string | number | null | undefined): string {
  const d = toDate(input)
  return d ? formatDateFns(d, 'dd-MM-yyyy') : '—'
}

/** DD-MM-YYYY HH:mm (24h, e.g. "18-04-2026 21:45"). Returns "—" for invalid input. */
export function formatDateTimeDDMMYYYY(input: Date | string | number | null | undefined): string {
  const d = toDate(input)
  return d ? formatDateFns(d, 'dd-MM-yyyy HH:mm') : '—'
}

/**
 * Parses a DD-MM-YYYY string into a Date. Returns null when the string is
 * empty, malformed, or represents an invalid calendar date.
 */
export function parseDDMMYYYY(input: string | null | undefined): Date | null {
  if (!input) return null
  const trimmed = input.trim()
  if (trimmed === '') return null
  const parsed = parseDateFns(trimmed, 'dd-MM-yyyy', new Date())
  return isValidDateFns(parsed) ? parsed : null
}

/** Serializes a Date as ISO YYYY-MM-DD for use in API params / URL state. */
export function toIsoDate(input: Date | string | number | null | undefined): string {
  const d = toDate(input)
  return d ? formatDateFns(d, 'yyyy-MM-dd') : ''
}

/**
 * Parses an ISO YYYY-MM-DD string into a *local* Date at midnight. Required
 * because `new Date('2026-04-19')` is interpreted as UTC midnight by the JS
 * spec, which becomes the previous calendar day in negative-offset
 * timezones — a classic off-by-one trap for date pickers.
 *
 * Returns null for empty / malformed / invalid-calendar-day input.
 */
export function parseIsoDate(input: string | null | undefined): Date | null {
  if (!input) return null
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(input.trim())
  if (!match) return null
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(year, month - 1, day)
  // Reject dates that overflowed (e.g. "2026-02-31" silently becomes March 3)
  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return null
  }
  return isValidDateFns(date) ? date : null
}

// ---------------------------------------------------------------------------
// Human-friendly date/time formatting.
//
// The all-numeric DD-MM-YYYY / HH:mm output reads as a wall of digits to
// non-technical floor managers. These helpers render weekday-prefixed dates
// (`Fri 9 May`) and 12-hour times (`4:42 PM`) for every screen the operator
// reads. The numeric DDMMYYYY/ISO helpers above are still used for typed
// inputs, URL state, and FBR/PRA fiscal receipts where the format is fixed.
// ---------------------------------------------------------------------------

interface FormatDateHumanOptions {
  /** Force the year suffix even when the date is in the current year. */
  alwaysYear?: boolean
}

function isSameLocalDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  )
}

/**
 * `Fri 9 May` for current-year dates, `Sat 28 Dec '25` when crossing years
 * (or when `alwaysYear` is set). Returns "—" for invalid input.
 */
export function formatDateHuman(
  input: Date | string | number | null | undefined,
  opts: FormatDateHumanOptions = {},
): string {
  const d = toDate(input)
  if (!d) return '—'
  const showYear = opts.alwaysYear || d.getFullYear() !== new Date().getFullYear()
  return showYear
    ? formatDateFns(d, "EEE d MMM ''yy")
    : formatDateFns(d, 'EEE d MMM')
}

/**
 * Smart range label collapses redundancy:
 *  - same day  → `Fri 9 May`
 *  - same month → `Sun 3 → Sat 9 May`
 *  - cross-month, same year → `Mon 28 Apr → Sun 4 May`
 *  - cross-year → `Sat 28 Dec '25 → Sun 5 Jan '26`
 */
export function formatDateRangeHuman(
  fromInput: Date | string | number | null | undefined,
  toInput: Date | string | number | null | undefined,
): string {
  const from = toDate(fromInput)
  const to = toDate(toInput)
  if (!from && !to) return '—'
  if (!from) return formatDateHuman(to)
  if (!to) return formatDateHuman(from)
  if (isSameLocalDay(from, to)) return formatDateHuman(from)

  const currentYear = new Date().getFullYear()
  const crossYear = from.getFullYear() !== to.getFullYear()
  const showYear = crossYear || from.getFullYear() !== currentYear || to.getFullYear() !== currentYear

  if (crossYear) {
    return `${formatDateFns(from, "EEE d MMM ''yy")} → ${formatDateFns(to, "EEE d MMM ''yy")}`
  }

  const sameMonth = from.getMonth() === to.getMonth()
  if (sameMonth) {
    const left = formatDateFns(from, 'EEE d')
    const right = showYear
      ? formatDateFns(to, "EEE d MMM ''yy")
      : formatDateFns(to, 'EEE d MMM')
    return `${left} → ${right}`
  }

  const left = showYear
    ? formatDateFns(from, "EEE d MMM ''yy")
    : formatDateFns(from, 'EEE d MMM')
  const right = showYear
    ? formatDateFns(to, "EEE d MMM ''yy")
    : formatDateFns(to, 'EEE d MMM')
  return `${left} → ${right}`
}

/** `4:42 PM` — 12-hour, no leading zero on the hour. */
export function formatTimeHuman(input: Date | string | number | null | undefined): string {
  const d = toDate(input)
  return d ? formatDateFns(d, 'h:mm a') : '—'
}

/**
 * Hour-only label for heatmap axes / peak badges.
 * Accepts an integer hour (0–23) and returns `4 AM`, `12 PM`, `7 PM`, etc.
 */
export function formatHour12(hour: number): string {
  if (!Number.isFinite(hour)) return '—'
  const h = ((Math.floor(hour) % 24) + 24) % 24
  const suffix = h < 12 ? 'AM' : 'PM'
  const display = h % 12 === 0 ? 12 : h % 12
  return `${display} ${suffix}`
}

/**
 * Business timezone — single frontend source of truth for "what does
 * 'local time' mean on this platform." Mirrors the backend's hardcoded
 * util.BusinessTimezone constant. Used by code that has to bucket or
 * label by local hour without relying on the browser's locale (because
 * the same calendar moment must label identically whether the user is
 * in Karachi, Lahore, or — for some support-tooling case — overseas).
 */
export const BUSINESS_TIMEZONE = 'Asia/Karachi'

// Single cached formatter — Intl.DateTimeFormat construction isn't free
// and this is called once per series bucket on every Day Close render.
const businessHourFormatter = new Intl.DateTimeFormat('en-US', {
  timeZone: BUSINESS_TIMEZONE,
  hour: 'numeric',
  hour12: false,
})

/**
 * Returns the 0–23 hour of `input` as observed in the business timezone.
 * Used by the hourly-sales chart to map each bucket's `hour_start` ISO
 * timestamp to its local hour, instead of inferring hour from the bucket's
 * position in the array (which silently drifts when the backend's series
 * window shifts).
 *
 * Some locales return "24" for midnight; we normalise via `% 24` so the
 * downstream chart can always index a length-24 array safely.
 */
export function hourInBusinessTimezone(input: Date | string | number): number {
  const d = input instanceof Date ? input : new Date(input)
  if (Number.isNaN(d.getTime())) return 0
  const raw = parseInt(businessHourFormatter.format(d), 10)
  return Number.isFinite(raw) ? ((raw % 24) + 24) % 24 : 0
}

/** `Fri 9 May, 4:42 PM` — full human date + time, comma-separated. */
export function formatDateTimeHuman(input: Date | string | number | null | undefined): string {
  const d = toDate(input)
  if (!d) return '—'
  return `${formatDateHuman(d)}, ${formatTimeHuman(d)}`
}

/**
 * Activity-feed timestamps. Within the last 24h we render relative phrasing
 * (`Just now`, `7m ago`, `2h ago`, `Yesterday, 9:15 AM`) so the user can
 * scan recent actions; older than 7 days falls back to the full
 * `Fri 9 May, 4:42 PM` label.
 */
export function formatRelative(input: Date | string | number | null | undefined): string {
  const d = toDate(input)
  if (!d) return '—'
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffSec = Math.round(diffMs / 1000)
  const diffMin = Math.round(diffSec / 60)
  const diffHr = Math.round(diffMin / 60)

  if (diffSec < 30 && diffSec > -30) return 'Just now'
  if (diffMin > 0 && diffMin < 60) return `${diffMin}m ago`
  if (diffHr > 0 && diffHr < 24 && isSameLocalDay(d, now)) return `${diffHr}h ago`

  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (isSameLocalDay(d, yesterday)) return `Yesterday, ${formatTimeHuman(d)}`

  if (isSameLocalDay(d, now)) return `Today, ${formatTimeHuman(d)}`

  return formatDateTimeHuman(d)
}

/**
 * Backwards-compatible: legacy callers pass an ISO timestamp string and
 * expect a "date + time" label. Now returns the human form so any callsite
 * we miss inherits the new format automatically.
 */
export function formatDate(dateString: string): string {
  return formatDateTimeHuman(dateString)
}

/** Time-only label. Now returns `4:42 PM` 12-hour format. */
export function formatTime(dateString: string): string {
  return formatTimeHuman(dateString)
}






export function debounce<T extends (...args: any[]) => any>(
  func: T,
  wait: number
): (...args: Parameters<T>) => void {
  let timeout: ReturnType<typeof setTimeout> | null = null
  
  return (...args: Parameters<T>) => {
    if (timeout) clearTimeout(timeout)
    timeout = setTimeout(() => func(...args), wait)
  }
}

