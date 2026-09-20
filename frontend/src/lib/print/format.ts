/**
 * Formatting shared by the three print builders. Everything here is pure, so
 * the builders stay snapshot-testable in vitest's node environment.
 *
 * Two rules this file exists to enforce:
 *
 *  1. **Escape everything.** A receipt interpolates business name, customer
 *     name, product names, payment references, closing notes and free-text
 *     void reasons into HTML that is then written into an iframe with
 *     `document.write`. A customer called `<b>` must not be able to reformat
 *     the slip, and a note containing `</style>` must not be able to break it.
 *     `esc()` is applied at every interpolation, without exception.
 *
 *  2. **A business date is not a timestamp.** `business_date` is a bare
 *     `YYYY-MM-DD` the server already resolved in Asia/Karachi. Passing it
 *     through `new Date()` parses it as UTC midnight and then renders it in
 *     the viewer's zone, which turns 2026-09-20 into 2026-09-19 anywhere west
 *     of Greenwich. `formatBusinessDate` only ever reads the string.
 */

/** HTML-escape an interpolated value. Null and undefined render as empty. */
export function esc(value: unknown): string {
  if (value === null || value === undefined) return ''
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/** A bare two-decimal amount with thousands separators: `3,312.50`. */
export function amount2(n: number): string {
  if (!Number.isFinite(n)) return '0.00'
  return n.toLocaleString('en-PK', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

/** A signed two-decimal amount, used for the rounding line: `+0.50`, `-0.25`. */
export function signedAmount2(n: number): string {
  const sign = n > 0 ? '+' : n < 0 ? '-' : ''
  return sign + amount2(Math.abs(n))
}

/**
 * A tax fraction as a label: 0.18 -> `18%`, 0.185 -> `18.5%`. Invoice and
 * settings tax rates are stored as fractions (see `lib/pricing.ts`).
 */
export function percentLabel(fraction: number): string {
  if (!Number.isFinite(fraction)) return '0%'
  return Number((fraction * 100).toFixed(4)) + '%'
}

/**
 * A number that is already a percentage as a label: 5 -> `5%`. `invoices
 * .discount_percent` is stored this way (5 for 5%, not 0.05), unlike the tax
 * rates above — the two scales sit one line apart on the receipt, so they get
 * two clearly named helpers rather than a division at the call site.
 */
export function plainPercentLabel(pct: number): string {
  if (!Number.isFinite(pct)) return '0%'
  return Number(pct.toFixed(4)) + '%'
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/**
 * `2026-09-20` -> `20 Sep 2026`, read straight off the string. Anything that
 * is not a leading `YYYY-MM-DD` is passed through untouched.
 */
export function formatBusinessDate(date: string | null | undefined): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(date ?? '')
  if (!m) return date ?? ''
  const month = MONTHS[Number(m[2]) - 1] ?? m[2]
  return m[3] + ' ' + month + ' ' + m[1]
}

/**
 * The Asia/Karachi wall-clock parts of an instant.
 *
 * `formatToParts` rather than `toLocaleString` because the composed string is
 * ICU-version dependent — the same call renders `20-Sept-2026` on one Node and
 * `20 Sep 2026` on another, which is a receipt that changes shape when the
 * till's runtime is upgraded and a test that fails for no reason. Pulling the
 * numeric parts out and spelling the month from our own table pins the layout;
 * the timezone conversion, which is the part that actually matters, is still
 * ICU's.
 */
function karachiParts(d: Date): Partial<Record<Intl.DateTimeFormatPartTypes, string>> {
  const fmt = new Intl.DateTimeFormat('en-PK', {
    timeZone: 'Asia/Karachi',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: true,
  })
  const out: Partial<Record<Intl.DateTimeFormatPartTypes, string>> = {}
  for (const part of fmt.formatToParts(d)) out[part.type] = part.value
  return out
}

function clockOf(p: Partial<Record<Intl.DateTimeFormatPartTypes, string>>): string {
  const period = (p.dayPeriod ?? '').toLowerCase().replace(/[^a-z]/g, '')
  return (p.hour ?? '') + ':' + (p.minute ?? '') + (period ? ' ' + period : '')
}

/** An ISO timestamp rendered in Asia/Karachi: `20 Sep 2026, 03:45 pm`. */
export function formatDateTimePK(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = karachiParts(d)
  const month = MONTHS[Number(p.month) - 1] ?? p.month ?? ''
  return (p.day ?? '') + ' ' + month + ' ' + (p.year ?? '') + ', ' + clockOf(p)
}

/** Just the clock part in Asia/Karachi: `03:45 pm`. */
export function formatTimePK(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return clockOf(karachiParts(d))
}

/** The tender name as it prints. */
export function paymentMethodLabel(method: string): string {
  switch (method) {
    case 'cash':
      return 'Cash'
    case 'card':
      return 'Card'
    case 'online':
      return 'Online'
    case 'credit':
      return 'Credit (on account)'
    default:
      return method
  }
}

/** `easypaisa` -> `Easypaisa`, `bank_transfer` -> `Bank transfer`. */
export function subMethodLabel(sub: string | null | undefined): string {
  if (!sub) return ''
  const spaced = sub.replace(/_/g, ' ')
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

/** Drops blanks and escapes what is left — used for the header/footer lines. */
export function escapedLines(lines: string[] | null | undefined): string[] {
  if (!Array.isArray(lines)) return []
  return lines.map((l) => (l ?? '').trim()).filter((l) => l.length > 0).map(esc)
}
