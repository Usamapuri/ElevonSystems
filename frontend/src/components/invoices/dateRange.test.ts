import { describe, expect, it } from 'vitest'
import {
  DATE_PRESETS,
  REPORT_DATE_PRESETS,
  matchPreset,
  presetLabel,
  presetRange,
  rangeInverted,
  rangeParams,
  shiftDateKey,
} from './dateRange'

/**
 * A UTC instant for a given Asia/Karachi (UTC+5, no DST) wall-clock moment
 * (same convention as dayGate.test.ts): businessDateKey is pinned to
 * Asia/Karachi via Intl, so these tests build instants from the business
 * timezone's clock rather than `new Date(y, m, d, h)`, which is the machine's
 * local clock and would make the suite's pass/fail depend on where it runs.
 */
function karachi(y: number, m: number, d: number, h: number, min = 0): Date {
  return new Date(Date.UTC(y, m - 1, d, h - 5, min))
}

/** Karachi noon, so the boundary-hour shift never crosses a date by accident. */
const noon = karachi(2026, 9, 20, 12, 0)

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
    const earlyHours = karachi(2026, 9, 20, 3, 0)
    expect(presetRange('today', earlyHours, 6)).toEqual({ from: '2026-09-19', to: '2026-09-19' })
    expect(presetRange('last7', earlyHours, 6)).toEqual({ from: '2026-09-13', to: '2026-09-19' })
  })
  it('has no computable range for a custom one', () => {
    expect(presetRange('custom', noon, 0)).toBeNull()
  })
  it('this_week starts Monday and never reaches past today', () => {
    // 2026-09-16 is a Wednesday; the week's Monday is 2026-09-14.
    const wednesday = karachi(2026, 9, 16, 12, 0)
    expect(presetRange('this_week', wednesday, 0)).toEqual({ from: '2026-09-14', to: '2026-09-16' })
  })
  it('this_week rolls back onto a Monday when today already is one', () => {
    const monday = karachi(2026, 9, 14, 12, 0)
    expect(presetRange('this_week', monday, 0)).toEqual({ from: '2026-09-14', to: '2026-09-14' })
  })
  it('this_month starts the 1st and is clipped to today', () => {
    expect(presetRange('this_month', noon, 0)).toEqual({ from: '2026-09-01', to: '2026-09-20' })
  })
  it('last_month is the previous calendar month in full', () => {
    expect(presetRange('last_month', noon, 0)).toEqual({ from: '2026-08-01', to: '2026-08-31' })
  })
  it('last_month crosses a year boundary', () => {
    const midJan = karachi(2026, 1, 15, 12, 0)
    expect(presetRange('last_month', midJan, 0)).toEqual({ from: '2025-12-01', to: '2025-12-31' })
  })
  it('last_month lands on a leap-year February', () => {
    const earlyMarch2024 = karachi(2024, 3, 1, 12, 0)
    expect(presetRange('last_month', earlyMarch2024, 0)).toEqual({ from: '2024-02-01', to: '2024-02-29' })
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
  it('checks the Reports screen preset set when it is passed in', () => {
    expect(matchPreset({ from: '2026-09-01', to: '2026-09-20' }, noon, 0, REPORT_DATE_PRESETS)).toBe('this_month')
    expect(matchPreset({ from: '2026-08-01', to: '2026-08-31' }, noon, 0, REPORT_DATE_PRESETS)).toBe('last_month')
    // Not any Reports preset for this date (2026-09-20 is a Sunday, so its
    // this_week happens to equal last7's span — pick a range none of them hit).
    expect(matchPreset({ from: '2026-09-10', to: '2026-09-20' }, noon, 0, REPORT_DATE_PRESETS)).toBe('custom')
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
  it('labels the Reports screen presets too', () => {
    expect(REPORT_DATE_PRESETS.map(presetLabel)).toEqual([
      'Today',
      'Yesterday',
      'This week',
      'This month',
      'Last month',
    ])
  })
})
