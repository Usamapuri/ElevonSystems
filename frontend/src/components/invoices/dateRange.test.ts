import { describe, expect, it } from 'vitest'
import {
  DATE_PRESETS,
  matchPreset,
  presetLabel,
  presetRange,
  rangeInverted,
  rangeParams,
  shiftDateKey,
} from './dateRange'

/** Local-time noon, so the boundary-hour shift never crosses a date by
 * accident on the machine running the suite (same convention as dayGate). */
const noon = new Date(2026, 8, 20, 12, 0, 0)

describe('shiftDateKey', () => {
  it('walks days without leaving the date domain', () => {
    expect(shiftDateKey('2026-09-20', -1)).toBe('2026-09-19')
    expect(shiftDateKey('2026-09-20', -6)).toBe('2026-09-14')
    expect(shiftDateKey('2026-09-20', 1)).toBe('2026-09-21')
  })
  it('crosses month, year and leap-day boundaries', () => {
    expect(shiftDateKey('2026-03-01', -1)).toBe('2026-02-28')
    expect(shiftDateKey('2024-03-01', -1)).toBe('2024-02-29')
    expect(shiftDateKey('2026-01-01', -1)).toBe('2025-12-31')
    expect(shiftDateKey('2026-12-31', 1)).toBe('2027-01-01')
  })
  it('passes anything that is not a date key straight through', () => {
    expect(shiftDateKey('', -1)).toBe('')
    expect(shiftDateKey('not-a-date', -1)).toBe('not-a-date')
  })
})

describe('presetRange', () => {
  it('is a single day for today and yesterday', () => {
    expect(presetRange('today', noon, 0)).toEqual({ from: '2026-09-20', to: '2026-09-20' })
    expect(presetRange('yesterday', noon, 0)).toEqual({ from: '2026-09-19', to: '2026-09-19' })
  })
  it('counts the last 7 days inclusive of today', () => {
    expect(presetRange('last7', noon, 0)).toEqual({ from: '2026-09-14', to: '2026-09-20' })
  })
  it('follows the business-day boundary hour, not the wall clock', () => {
    // 03:00 with a 06:00 boundary is still the previous business day.
    const earlyHours = new Date(2026, 8, 20, 3, 0, 0)
    expect(presetRange('today', earlyHours, 6)).toEqual({ from: '2026-09-19', to: '2026-09-19' })
    expect(presetRange('last7', earlyHours, 6)).toEqual({ from: '2026-09-13', to: '2026-09-19' })
  })
  it('has no computable range for a custom one', () => {
    expect(presetRange('custom', noon, 0)).toBeNull()
  })
})

describe('matchPreset', () => {
  it('lights the button a range corresponds to', () => {
    expect(matchPreset({ from: '2026-09-20', to: '2026-09-20' }, noon, 0)).toBe('today')
    expect(matchPreset({ from: '2026-09-19', to: '2026-09-19' }, noon, 0)).toBe('yesterday')
    expect(matchPreset({ from: '2026-09-14', to: '2026-09-20' }, noon, 0)).toBe('last7')
  })
  it('falls back to custom for anything else', () => {
    expect(matchPreset({ from: '2026-09-01', to: '2026-09-20' }, noon, 0)).toBe('custom')
    expect(matchPreset({ from: '', to: '' }, noon, 0)).toBe('custom')
  })
})

describe('range plumbing', () => {
  it('flags an inverted range instead of showing an empty page', () => {
    expect(rangeInverted({ from: '2026-09-20', to: '2026-09-01' })).toBe(true)
    expect(rangeInverted({ from: '2026-09-01', to: '2026-09-20' })).toBe(false)
    expect(rangeInverted({ from: '2026-09-20', to: '2026-09-20' })).toBe(false)
    expect(rangeInverted({ from: '', to: '2026-09-20' })).toBe(false)
  })
  it('sends only the ends that are real dates', () => {
    expect(rangeParams({ from: '2026-09-14', to: '2026-09-20' })).toEqual({ from: '2026-09-14', to: '2026-09-20' })
    expect(rangeParams({ from: '', to: '2026-09-20' })).toEqual({ to: '2026-09-20' })
    expect(rangeParams({ from: '', to: '' })).toEqual({})
    expect(rangeParams({ from: '2026-9-1', to: '' })).toEqual({})
  })
  it('labels every preset it offers', () => {
    expect(DATE_PRESETS.map(presetLabel)).toEqual(['Today', 'Yesterday', 'Last 7 days'])
  })
})
