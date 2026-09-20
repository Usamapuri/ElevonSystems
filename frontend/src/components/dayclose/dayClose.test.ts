import { describe, expect, it } from 'vitest'
import {
  DEFAULT_VARIANCE_THRESHOLD,
  emptyCounts,
  evaluateClose,
  normalizeThreshold,
  overThreshold,
  parseCounted,
  parseMovementAmount,
  parseOpeningCash,
  round2,
  tenderLabel,
  TENDERS,
} from './dayClose'
import type { DayExpected } from '@/types'

function expected(over: Partial<DayExpected> = {}): DayExpected {
  return {
    opening_cash: 5000,
    cash_sales: 10000,
    card_sales: 2000,
    online_sales: 1500,
    on_account_sales: 3000,
    cash_receipts: 0,
    card_receipts: 0,
    online_receipts: 0,
    receipts_collected: 0,
    paid_in: 0,
    paid_out: 500,
    cash: 14500,
    card: 2000,
    online: 1500,
    gross_sales: 16500,
    discounts: 0,
    tax_collected: 0,
    net_sales: 16500,
    invoice_count: 7,
    void_count: 0,
    ...over,
  }
}

describe('parseCounted', () => {
  it('reads a plain amount', () => {
    expect(parseCounted('14500')).toBe(14500)
    expect(parseCounted(' 250.25 ')).toBe(250.25)
    expect(parseCounted('0')).toBe(0)
  })
  it('tolerates a half-typed decimal point', () => {
    expect(parseCounted('12.')).toBe(12)
  })
  it('is null for "not counted yet", never zero', () => {
    expect(parseCounted('')).toBeNull()
    expect(parseCounted('   ')).toBeNull()
    expect(parseCounted('.')).toBeNull()
    expect(parseCounted('abc')).toBeNull()
  })
  it('refuses what the server refuses: negatives and sub-paisa', () => {
    expect(parseCounted('-5')).toBeNull()
    expect(parseCounted('12.345')).toBeNull()
  })
})

describe('round2 and overThreshold mirror the server', () => {
  it('rounds to paisa the way dayops.round2 does', () => {
    expect(round2(14500.004)).toBe(14500)
    expect(round2(14500.006)).toBe(14500.01)
    expect(round2(0.1 + 0.2)).toBe(0.3)
    // Deliberately the SAME float artifact as Go: 1.005*100 is
    // 100.49999999999999 in float64, so math.Round takes it down. This
    // mirrors the server rather than out-rounding it — lib/pricing.ts owns
    // the careful half-up round2 for invoice money, and a close form that
    // rounded differently from the transaction sealing the day would show a
    // variance the server disagreed with.
    expect(round2(1.005)).toBe(1)
  })
  it('uses a strict comparison with a paisa epsilon', () => {
    // dayops.overThreshold: |v| > threshold + 1e-9. Exactly on the threshold
    // is inside it, so the note box must not light up.
    expect(overThreshold(100, 100)).toBe(false)
    expect(overThreshold(-100, 100)).toBe(false)
    expect(overThreshold(100.01, 100)).toBe(true)
    expect(overThreshold(-100.01, 100)).toBe(true)
  })
  it('falls back rather than blocking a close on a busted setting', () => {
    expect(normalizeThreshold(-1)).toBe(DEFAULT_VARIANCE_THRESHOLD)
    expect(normalizeThreshold(NaN)).toBe(DEFAULT_VARIANCE_THRESHOLD)
    expect(normalizeThreshold(null)).toBe(DEFAULT_VARIANCE_THRESHOLD)
    expect(normalizeThreshold(0)).toBe(0)
    expect(normalizeThreshold(250)).toBe(250)
  })
})

describe('evaluateClose', () => {
  it('cannot submit while any tender is uncounted', () => {
    const e = evaluateClose({ expected: expected(), counted: emptyCounts, threshold: 100, notes: '' })
    expect(e.complete).toBe(false)
    expect(e.canSubmit).toBe(false)
    expect(e.payload).toBeNull()
    expect(e.tenders.map((t) => t.counted)).toEqual([null, null, null])
    expect(e.tenders.map((t) => t.variance)).toEqual([null, null, null])
  })

  it('counts a zero as counted', () => {
    const e = evaluateClose({
      expected: expected({ cash: 0, card: 0, online: 0 }),
      counted: { cash: '0', card: '0', online: '0' },
      threshold: 100,
      notes: '',
    })
    expect(e.complete).toBe(true)
    expect(e.noteRequired).toBe(false)
    expect(e.canSubmit).toBe(true)
    expect(e.payload).toEqual({ counted_cash: 0, counted_card: 0, counted_online: 0 })
  })

  it('computes counted − expected per tender', () => {
    const e = evaluateClose({
      expected: expected(),
      counted: { cash: '14450', card: '2000', online: '1500' },
      threshold: 100,
      notes: '',
    })
    expect(e.tenders.map((t) => t.variance)).toEqual([-50, 0, 0])
    expect(e.noteRequired).toBe(false)
    expect(e.canSubmit).toBe(true)
  })

  it('requires a note once a tender is over the threshold, and only then', () => {
    const over = {
      expected: expected(),
      counted: { cash: '14500', card: '2000', online: '1350' },
      threshold: 100,
      notes: '',
    }
    const e = evaluateClose(over)
    expect(e.tenders[2].variance).toBe(-150)
    expect(e.tenders[2].overThreshold).toBe(true)
    expect(e.noteRequired).toBe(true)
    expect(e.canSubmit).toBe(false)

    const withNote = evaluateClose({ ...over, notes: '  Refund paid back in cash  ' })
    expect(withNote.canSubmit).toBe(true)
    expect(withNote.payload).toEqual({
      counted_cash: 14500,
      counted_card: 2000,
      counted_online: 1350,
      closing_notes: 'Refund paid back in cash',
    })
  })

  it('does not light up on a variance exactly on the threshold', () => {
    const e = evaluateClose({
      expected: expected(),
      counted: { cash: '14600', card: '2000', online: '1500' },
      threshold: 100,
      notes: '',
    })
    expect(e.tenders[0].variance).toBe(100)
    expect(e.noteRequired).toBe(false)
    expect(e.canSubmit).toBe(true)
  })

  it('never puts a string in the payload', () => {
    const e = evaluateClose({
      expected: expected(),
      counted: { cash: '14500.50', card: '2000', online: '1500' },
      threshold: 100,
      notes: '',
    })
    for (const v of Object.values(e.payload ?? {})) {
      if (typeof v === 'string') continue // closing_notes is text
      expect(typeof v).toBe('number')
    }
    expect(e.payload?.counted_cash).toBe(14500.5)
  })

  it('treats a missing expectation as zero rather than NaN', () => {
    const e = evaluateClose({ expected: null, counted: { cash: '0', card: '0', online: '0' }, threshold: 100, notes: '' })
    expect(e.tenders.map((t) => t.expected)).toEqual([0, 0, 0])
    expect(e.tenders.map((t) => t.variance)).toEqual([0, 0, 0])
    expect(Number.isNaN(e.tenders[0].variance)).toBe(false)
  })
})

describe('the smaller parsers', () => {
  it('allows a declared float of zero but not a movement of zero', () => {
    expect(parseOpeningCash('0')).toBe(0)
    expect(parseMovementAmount('0')).toBeNull()
    expect(parseMovementAmount('250')).toBe(250)
    expect(parseMovementAmount('')).toBeNull()
  })
})

describe('tender vocabulary', () => {
  it('covers exactly the three counted tenders', () => {
    expect([...TENDERS]).toEqual(['cash', 'card', 'online'])
    expect(TENDERS.map(tenderLabel)).toEqual(['Cash', 'Card', 'Online'])
  })
})
