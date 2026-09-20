import { describe, expect, it } from 'vitest'
import {
  formatKg,
  isLineInputError,
  kgFromAmount,
  kgFromTonne,
  modeLabel,
  netFromGrossTare,
  round3,
  toLineInput,
  type LineInput,
} from './weight'

/** Narrows past the error branch so the assertions below read straight. */
function ok(r: ReturnType<typeof toLineInput>): LineInput {
  if (isLineInputError(r)) throw new Error(`expected a line, got error: ${r.error}`)
  return r
}

function err(r: ReturnType<typeof toLineInput>): string {
  if (!isLineInputError(r)) throw new Error(`expected an error, got quantity ${r.quantity}`)
  return r.error
}

describe('round3', () => {
  it('keeps three decimals', () => {
    expect(round3(12.5)).toBe(12.5)
    expect(round3(12.3456)).toBe(12.346)
  })
  it('rounds halves up, including the float ties a naive *1000 misrounds', () => {
    expect(round3(0.0005)).toBe(0.001)
    expect(round3(1.0005)).toBe(1.001)
    expect(round3(2.3455)).toBe(2.346)
  })
  it('handles negatives symmetrically', () => {
    expect(round3(-0.0005)).toBe(-0.001)
  })
  it('returns NaN for non-finite input', () => {
    expect(Number.isNaN(round3(NaN))).toBe(true)
    expect(Number.isNaN(round3(Infinity))).toBe(true)
  })
})

describe('kgFromTonne', () => {
  it('converts tonnes to kg', () => {
    expect(kgFromTonne(1)).toBe(1000)
    expect(kgFromTonne(1.5)).toBe(1500)
    expect(kgFromTonne(0.0125)).toBe(12.5)
  })
  it('rounds the result to 3 dp', () => {
    expect(kgFromTonne(0.0000004)).toBe(0)
    expect(kgFromTonne(0.0000005)).toBe(0.001)
  })
})

describe('kgFromAmount', () => {
  it('divides the rupee amount by the rate', () => {
    expect(kgFromAmount(3312.5, 265)).toBe(12.5)
    expect(kgFromAmount(1000, 265)).toBe(3.774)
  })
  it('is NaN for a rate of zero or below — callers must reject it', () => {
    expect(Number.isNaN(kgFromAmount(1000, 0))).toBe(true)
    expect(Number.isNaN(kgFromAmount(1000, -5))).toBe(true)
  })
})

describe('netFromGrossTare', () => {
  it('subtracts and rounds', () => {
    expect(netFromGrossTare(50.5, 12.25)).toBe(38.25)
    expect(netFromGrossTare(0.3, 0.1)).toBe(0.2) // float noise cleaned
  })
})

describe('formatKg', () => {
  it('always shows three decimals and no unit or separator', () => {
    expect(formatKg(12.5)).toBe('12.500')
    expect(formatKg(1250)).toBe('1250.000')
    expect(formatKg(0)).toBe('0.000')
  })
  it('degrades to 0.000 rather than "NaN"', () => {
    expect(formatKg(NaN)).toBe('0.000')
  })
})

describe('toLineInput — kg mode', () => {
  it('takes the typed kg straight', () => {
    expect(ok(toLineInput('kg', { value: '12.5' }, 265))).toEqual({ quantity: 12.5, entered_as: 'kg' })
  })
  it('rounds to 3 dp', () => {
    expect(ok(toLineInput('kg', { value: '12.34567' }, 265)).quantity).toBe(12.346)
  })
  it('rejects blank, non-numeric, zero and negative', () => {
    expect(err(toLineInput('kg', {}, 265))).toMatch(/Enter a weight/)
    expect(err(toLineInput('kg', { value: '  ' }, 265))).toMatch(/Enter a weight/)
    expect(err(toLineInput('kg', { value: 'abc' }, 265))).toMatch(/Enter a weight/)
    expect(err(toLineInput('kg', { value: '0' }, 265))).toMatch(/more than zero/)
    expect(err(toLineInput('kg', { value: '-3' }, 265))).toMatch(/more than zero/)
  })
  it('rejects a weight past the server ceiling', () => {
    expect(err(toLineInput('kg', { value: '1e9' }, 265))).toMatch(/too large/)
  })
})

describe('toLineInput — tonne mode', () => {
  it('converts and records the mode', () => {
    expect(ok(toLineInput('tonne', { value: '1.5' }, 265))).toEqual({ quantity: 1500, entered_as: 'tonne' })
  })
  it('rejects zero tonnes', () => {
    expect(err(toLineInput('tonne', { value: '0' }, 265))).toMatch(/more than zero/)
  })
})

describe('toLineInput — amount mode', () => {
  it('converts rupees to kg at the product rate', () => {
    expect(ok(toLineInput('amount', { value: '3312.50' }, 265))).toEqual({ quantity: 12.5, entered_as: 'amount' })
  })
  it('refuses a product with no usable rate', () => {
    expect(err(toLineInput('amount', { value: '1000' }, 0))).toMatch(/no rate/)
    expect(err(toLineInput('amount', { value: '1000' }, -1))).toMatch(/no rate/)
    expect(err(toLineInput('amount', { value: '1000' }, NaN))).toMatch(/no rate/)
  })
  it('rejects a zero amount', () => {
    expect(err(toLineInput('amount', { value: '0' }, 265))).toMatch(/more than zero/)
  })
  it('produces a quantity the server will accept (at most 3 dp)', () => {
    const line = ok(toLineInput('amount', { value: '1000' }, 265))
    const scaled = line.quantity * 1000
    expect(Math.abs(scaled - Math.round(scaled))).toBeLessThan(1e-6)
  })
})

describe('toLineInput — gross − tare mode', () => {
  it('returns the net plus both weights', () => {
    expect(ok(toLineInput('gross_tare', { gross: '50.5', tare: '12.25' }, 265))).toEqual({
      quantity: 38.25,
      entered_as: 'gross_tare',
      gross_weight: 50.5,
      tare_weight: 12.25,
    })
  })
  it('rounds both weights before subtracting, so gross − tare === quantity', () => {
    const line = ok(toLineInput('gross_tare', { gross: '50.5004', tare: '12.2496' }, 265))
    expect(line.gross_weight).toBe(50.5)
    expect(line.tare_weight).toBe(12.25)
    // The server re-checks |(gross − tare) − quantity| <= 0.0005.
    const gap = Math.abs((line.gross_weight as number) - (line.tare_weight as number) - line.quantity)
    expect(gap).toBeLessThan(0.0005)
  })
  it('rejects gross equal to or below tare', () => {
    expect(err(toLineInput('gross_tare', { gross: '12', tare: '12' }, 265))).toMatch(/more than tare/)
    expect(err(toLineInput('gross_tare', { gross: '10', tare: '12' }, 265))).toMatch(/more than tare/)
  })
  it('rejects a missing or negative weight', () => {
    expect(err(toLineInput('gross_tare', { tare: '12' }, 265))).toMatch(/gross weight/)
    expect(err(toLineInput('gross_tare', { gross: '50' }, 265))).toMatch(/tare weight/)
    expect(err(toLineInput('gross_tare', { gross: '50', tare: '-1' }, 265))).toMatch(/negative/)
  })
})

describe('modeLabel', () => {
  it('names every mode', () => {
    expect(modeLabel('kg')).toBe('kg')
    expect(modeLabel('tonne')).toBe('tonne')
    expect(modeLabel('amount')).toBe('amount')
    expect(modeLabel('gross_tare')).toBe('gross − tare')
  })
})
