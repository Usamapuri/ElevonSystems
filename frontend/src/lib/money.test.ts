import { describe, expect, it } from 'vitest'
import { compactMoney, formatKgGrouped, formatKgTick, formatMoney, signedMoney } from './money'

describe('formatMoney', () => {
  it('formats whole rupees with thousands separators', () => {
    expect(formatMoney(1234567)).toBe('Rs 1,234,567')
  })
  it('keeps paisa when present', () => {
    expect(formatMoney(12.5)).toBe('Rs 12.50')
  })
  it('handles negatives', () => {
    expect(formatMoney(-300)).toBe('-Rs 300')
  })
})

describe('signedMoney', () => {
  it('signs a surplus and a shortfall explicitly', () => {
    expect(signedMoney(250)).toBe('+Rs 250')
    expect(signedMoney(-250)).toBe('-Rs 250')
  })
  it('leaves zero unsigned', () => {
    expect(signedMoney(0)).toBe('Rs 0')
  })
  it('renders an em dash for a figure nobody counted', () => {
    expect(signedMoney(null)).toBe('—')
    expect(signedMoney(undefined)).toBe('—')
    expect(signedMoney(NaN)).toBe('—')
  })
})

describe('formatKgGrouped', () => {
  it('always shows three decimals and the unit', () => {
    expect(formatKgGrouped(12.5)).toBe('12.500 kg')
    expect(formatKgGrouped(1250)).toBe('1,250.000 kg')
  })
})

describe('formatKgTick', () => {
  it('rounds to whole kilos with no unit', () => {
    expect(formatKgTick(12.5)).toBe('13')
    expect(formatKgTick(1250.333)).toBe('1,250')
  })
})

describe('compactMoney', () => {
  it('keeps small amounts whole', () => {
    expect(compactMoney(0)).toBe('0')
    expect(compactMoney(999)).toBe('999')
  })
  it('compacts thousands to one decimal K', () => {
    expect(compactMoney(12300)).toBe('12.3K')
    expect(compactMoney(1000)).toBe('1.0K')
  })
  it('compacts millions to one decimal M', () => {
    expect(compactMoney(4200000)).toBe('4.2M')
  })
  it('handles negatives', () => {
    expect(compactMoney(-12300)).toBe('-12.3K')
  })
})
