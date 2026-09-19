import { describe, expect, it } from 'vitest'
import { formatMoney, formatKg } from './money'

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

describe('formatKg', () => {
  it('always shows three decimals and the unit', () => {
    expect(formatKg(12.5)).toBe('12.500 kg')
    expect(formatKg(1250)).toBe('1,250.000 kg')
  })
})
