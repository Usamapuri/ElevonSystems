/**
 * Pure derivations for the dashboard's KPI tiles (spec §3, Task P4). Kept out
 * of KpiRow so they are unit-testable without mounting React, and so a future
 * report screen can reuse the same arithmetic instead of a second copy that
 * could round differently.
 */
import type { TenderSplit } from '@/types'

/** Net revenue divided by invoice count. 0, not NaN or Infinity, when the
 * window rang no invoices — an owner reading "Rs 0" understands "nothing
 * sold"; an owner reading "NaN" thinks the screen is broken. */
export function averageInvoice(net: number, invoices: number): number {
  return invoices > 0 ? net / invoices : 0
}

const TENDER_KEYS = ['cash', 'card', 'online', 'on_account'] as const

/** One tender's slice of the mix: the rupee amount and its share of the
 * total, as a whole-number percentage. */
export interface TenderMixEntry {
  amount: number
  pct: number
}

export type TenderMix = Record<(typeof TENDER_KEYS)[number], TenderMixEntry>

/**
 * Splits a TenderSplit into whole-number percentages of cash + card + online
 * + on_account, which by construction equals `PeriodSummary.net`
 * (backend/internal/reports/summary.go: every completed invoice's
 * total_payable lands in exactly one of the four tender buckets) — so
 * on-account is one of the four shares, not a figure added on top of it.
 *
 * Rounding each share independently (`Math.round`) can miss 100 by a point
 * in either direction — four tenders at 33.3% each round to 99%, and a
 * dashboard an owner checks arithmetic on should not show percentages that
 * do not add up. This uses the largest-remainder method (Hamilton's
 * apportionment): floor every share, then hand the leftover point(s) — at
 * most three, one per tender — to the shares with the largest fractional
 * remainder, so the four percentages always sum to exactly 100 whenever the
 * total is positive.
 *
 * A non-positive total (nothing rang yet) returns every percentage as 0
 * rather than dividing by zero.
 */
export function tenderMix(tenders: TenderSplit): TenderMix {
  const total = tenders.cash + tenders.card + tenders.online + tenders.on_account

  if (!(total > 0)) {
    return TENDER_KEYS.reduce((acc, key) => {
      acc[key] = { amount: tenders[key], pct: 0 }
      return acc
    }, {} as TenderMix)
  }

  const shares = TENDER_KEYS.map((key) => (tenders[key] / total) * 100)
  const floors = shares.map(Math.floor)
  const remainders = shares.map((share, i) => ({ index: i, remainder: share - floors[i] }))
  remainders.sort((a, b) => b.remainder - a.remainder)

  let leftover = 100 - floors.reduce((sum, v) => sum + v, 0)
  const pct = [...floors]
  for (let i = 0; i < remainders.length && leftover > 0; i++, leftover--) {
    pct[remainders[i].index] += 1
  }

  return TENDER_KEYS.reduce((acc, key, i) => {
    acc[key] = { amount: tenders[key], pct: pct[i] }
    return acc
  }, {} as TenderMix)
}

/** `DailyRow.label` is `DD-MM-YYYY` (spec §6.8). The trend chart's x-axis
 * only has room for day and month across a 30-day window, so this reads the
 * first two hyphen groups straight off the string instead of reparsing a
 * date (business dates are strings on purpose — see lib/print/format.ts). */
export function dayMonthLabel(label: string): string {
  const m = /^(\d{2}-\d{2})-\d{4}$/.exec(label)
  return m ? m[1] : label
}
