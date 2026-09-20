import { describe, expect, it } from 'vitest'
import { averageInvoice, dayMonthLabel, tenderMix } from './kpis'
import type { TenderSplit } from '@/types'

function split(over: Partial<TenderSplit> = {}): TenderSplit {
  return { cash: 0, card: 0, online: 0, on_account: 0, ...over }
}

describe('averageInvoice', () => {
  it('divides net by invoice count', () => {
    expect(averageInvoice(10000, 4)).toBe(2500)
  })
  it('is 0, not NaN, when nothing rang', () => {
    expect(averageInvoice(0, 0)).toBe(0)
  })
  it('is 0 for a negative or zero invoice count too', () => {
    expect(averageInvoice(500, 0)).toBe(0)
  })
})

describe('tenderMix', () => {
  it('splits an even four-way mix without losing a point to rounding', () => {
    // 100/4 = 25 exactly — no remainder to redistribute.
    const mix = tenderMix(split({ cash: 100, card: 100, online: 100, on_account: 100 }))
    expect(mix.cash).toEqual({ amount: 100, pct: 25 })
    expect(mix.card).toEqual({ amount: 100, pct: 25 })
    expect(mix.online).toEqual({ amount: 100, pct: 25 })
    expect(mix.on_account).toEqual({ amount: 100, pct: 25 })
    expect(mix.cash.pct + mix.card.pct + mix.online.pct + mix.on_account.pct).toBe(100)
  })

  it('always sums to exactly 100 — the classic three-way-third case', () => {
    // 100/3 = 33.33... x3 — plain per-item rounding would land on 99 or 102.
    const mix = tenderMix(split({ cash: 100, card: 100, online: 100, on_account: 0 }))
    const total = mix.cash.pct + mix.card.pct + mix.online.pct + mix.on_account.pct
    expect(total).toBe(100)
    // Largest-remainder: all three ties get the leftover point, one each.
    expect(mix.cash.pct).toBe(34)
    expect(mix.card.pct).toBe(33)
    expect(mix.online.pct).toBe(33)
    expect(mix.on_account.pct).toBe(0)
  })

  it('includes on-account as one of the four shares, not added on top', () => {
    const mix = tenderMix(split({ cash: 300, on_account: 100 }))
    expect(mix.cash.pct).toBe(75)
    expect(mix.on_account.pct).toBe(25)
    expect(mix.cash.pct + mix.card.pct + mix.online.pct + mix.on_account.pct).toBe(100)
  })

  it('returns zero percentages (not NaN) when the total is zero', () => {
    const mix = tenderMix(split())
    expect(mix.cash).toEqual({ amount: 0, pct: 0 })
    expect(mix.on_account).toEqual({ amount: 0, pct: 0 })
  })

  it('carries the raw amount through unchanged', () => {
    const mix = tenderMix(split({ cash: 1234.56, card: 789 }))
    expect(mix.cash.amount).toBe(1234.56)
    expect(mix.card.amount).toBe(789)
  })
})

describe('dayMonthLabel', () => {
  it('drops the year from a DD-MM-YYYY label', () => {
    expect(dayMonthLabel('20-09-2026')).toBe('20-09')
  })
  it('passes an unrecognised string through untouched', () => {
    expect(dayMonthLabel('not-a-date')).toBe('not-a-date')
  })
})
