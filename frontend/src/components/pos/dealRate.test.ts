import { describe, expect, it } from 'vitest'
import { effectiveRate, needsPin, parseRateInput } from './dealRate'

describe('parseRateInput', () => {
  it('accepts a positive rupee rate to 2 dp', () => {
    expect(parseRateInput('249.5')).toBe(249.5)
    expect(parseRateInput(' 250 ')).toBe(250)
  })
  it('refuses everything else', () => {
    for (const bad of ['', '0', '-1', '1.234', 'abc', '1e3']) expect(parseRateInput(bad)).toBeNull()
  })
})

describe('needsPin', () => {
  it('is false at the list rate and inside the band', () => {
    expect(needsPin(250, 250, 0)).toBe(false)
    expect(needsPin(250, 249, 1)).toBe(false)
    expect(needsPin(250, 251, 1)).toBe(false)
  })
  it('is true one paisa past the band, either side', () => {
    expect(needsPin(250, 248.99, 1)).toBe(true)
    expect(needsPin(250, 251.01, 1)).toBe(true)
    expect(needsPin(250, 249.99, 0)).toBe(true)
  })
  it('does not trip on float representation', () => {
    expect(needsPin(0.3, 0.1 + 0.2, 0)).toBe(false)
  })
})

describe('effectiveRate', () => {
  it('prefers the deal rate', () => {
    expect(effectiveRate({ rate: 250 })).toBe(250)
    expect(effectiveRate({ rate: 250, rate_override: 249.5 })).toBe(249.5)
  })
})
