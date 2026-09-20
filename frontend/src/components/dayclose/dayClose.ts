/**
 * The close form's arithmetic, mirrored from `dayops.Close` (spec §6.7).
 *
 * The server is the authority: it recomputes Expected inside the closing
 * transaction, rounds the counts to paisa, and refuses the close with
 * `variance_note_required` when any tender is off by more than
 * `day_close_variance_threshold` and nothing explains it. Everything here
 * exists so the person counting sees the variance and the note requirement
 * while they are still typing, instead of discovering both from a 400.
 *
 * Two shapes are deliberate:
 *
 *  1. Counts are raw strings, not numbers. "12." is a real state of a
 *     half-typed amount that `Number()` would silently flatten to 12, and a
 *     blank box is "not counted yet", which is a different thing from 0 —
 *     the server stores NULL only for a force close, so a blank must never
 *     reach it as a zero.
 *  2. The rounding and the comparison are the server's, to the letter:
 *     round2 on the count, round2 on the difference, and |variance| >
 *     threshold + 1e-9. A form that used a bare `>` would light up the note
 *     box on a variance the server was about to wave through.
 */
import type { CloseDayRequest, DayExpected } from '@/types'

/** The three counted tenders, in the order the form shows them. Credit is
 * absent on purpose: an on-account sale puts no money in the till. */
export type TenderKey = 'cash' | 'card' | 'online'

export const TENDERS: readonly TenderKey[] = ['cash', 'card', 'online'] as const

export function tenderLabel(tender: TenderKey): string {
  switch (tender) {
    case 'cash':
      return 'Cash'
    case 'card':
      return 'Card'
    case 'online':
      return 'Online'
  }
}

/** What the three boxes hold while the person counts. */
export interface CountedInputs {
  cash: string
  card: string
  online: string
}

export const emptyCounts: CountedInputs = { cash: '', card: '', online: '' }

/** Matches `dayops.DefaultVarianceThreshold` and the seeded setting. */
export const DEFAULT_VARIANCE_THRESHOLD = 100

/** Paisa precision, the same way the server does it. */
export function round2(n: number): number {
  if (!Number.isFinite(n)) return NaN
  return Math.round(n * 100) / 100
}

/**
 * One counted box as a number, or null when it does not yet hold a usable
 * amount — blank, non-numeric, negative, or finer than paisa (the server
 * answers that last one with `invalid_counted_amount`).
 */
export function parseCounted(raw: string): number | null {
  const s = (raw ?? '').trim()
  if (s === '') return null
  if (!/^\d*(\.\d*)?$/.test(s) || s === '.') return null
  const dot = s.indexOf('.')
  if (dot >= 0 && s.length - dot - 1 > 2) return null
  const n = Number(s)
  if (!Number.isFinite(n) || n < 0) return null
  return round2(n)
}

/** |variance| against the threshold at paisa precision — `dayops.overThreshold`. */
export function overThreshold(variance: number, threshold: number): boolean {
  return Math.abs(variance) > threshold + 1e-9
}

/** A negative, NaN or missing threshold falls back rather than blocking a
 * close — the note gate exists to make a person explain a real discrepancy,
 * and a busted setting is not a reason to hold the till shut. */
export function normalizeThreshold(threshold: number | null | undefined): number {
  if (typeof threshold !== 'number' || !Number.isFinite(threshold) || threshold < 0) {
    return DEFAULT_VARIANCE_THRESHOLD
  }
  return threshold
}

export interface TenderVariance {
  tender: TenderKey
  expected: number
  /** null while the box is blank or half-typed — never 0. */
  counted: number | null
  /** counted − expected, null while nothing has been counted. */
  variance: number | null
  overThreshold: boolean
}

export interface CloseEvaluation {
  tenders: TenderVariance[]
  /** All three boxes hold a usable amount. */
  complete: boolean
  /** Some tender is over the threshold, so the server will insist on a note. */
  noteRequired: boolean
  /** Ready to post: complete, and carrying a note when one is required. */
  canSubmit: boolean
  /** Exactly what POST /day/close takes — numbers, never strings. Null until
   * the form is complete, so a half-counted day cannot be submitted. */
  payload: CloseDayRequest | null
}

function expectedFor(expected: DayExpected | null | undefined, tender: TenderKey): number {
  if (!expected) return 0
  const v = expected[tender]
  return Number.isFinite(v) ? round2(v) : 0
}

export function evaluateClose(args: {
  expected: DayExpected | null | undefined
  counted: CountedInputs
  threshold: number | null | undefined
  notes: string
}): CloseEvaluation {
  const threshold = normalizeThreshold(args.threshold)
  const tenders: TenderVariance[] = TENDERS.map((tender) => {
    const expected = expectedFor(args.expected, tender)
    const counted = parseCounted(args.counted[tender])
    const variance = counted === null ? null : round2(counted - expected)
    return {
      tender,
      expected,
      counted,
      variance,
      overThreshold: variance !== null && overThreshold(variance, threshold),
    }
  })

  const complete = tenders.every((t) => t.counted !== null)
  const noteRequired = tenders.some((t) => t.overThreshold)
  const note = (args.notes ?? '').trim()
  const canSubmit = complete && (!noteRequired || note !== '')

  const payload: CloseDayRequest | null = complete
    ? {
        counted_cash: tenders[0].counted as number,
        counted_card: tenders[1].counted as number,
        counted_online: tenders[2].counted as number,
        ...(note ? { closing_notes: note } : {}),
      }
    : null

  return { tenders, complete, noteRequired, canSubmit, payload }
}

/** A positive opening float, or null while the box is not a usable amount.
 * Zero is allowed — it is a real declaration about an empty drawer. */
export function parseOpeningCash(raw: string): number | null {
  return parseCounted(raw)
}

/** A movement amount has to be more than zero (handlers/dayops.go). */
export function parseMovementAmount(raw: string): number | null {
  const n = parseCounted(raw)
  if (n === null || n <= 0) return null
  return n
}
