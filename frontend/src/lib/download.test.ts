import { describe, expect, it } from 'vitest'
import { parseContentDispositionFilename } from './download'

describe('parseContentDispositionFilename', () => {
  it('reads the quoted filename the backend sends', () => {
    expect(parseContentDispositionFilename('attachment; filename="daily_2026-09-01_2026-09-20.csv"')).toBe(
      'daily_2026-09-01_2026-09-20.csv',
    )
  })
  it('reads an xlsx filename', () => {
    expect(parseContentDispositionFilename('attachment; filename="receivables_2026-09-20_2026-09-20.xlsx"')).toBe(
      'receivables_2026-09-20_2026-09-20.xlsx',
    )
  })
  it('falls back to a bare, unquoted filename', () => {
    expect(parseContentDispositionFilename('attachment; filename=daily.csv')).toBe('daily.csv')
  })
  it('stops a bare filename at the next parameter', () => {
    expect(parseContentDispositionFilename('attachment; filename=daily.csv; foo=bar')).toBe('daily.csv')
  })
  it('is null for a missing header', () => {
    expect(parseContentDispositionFilename(undefined)).toBeNull()
    expect(parseContentDispositionFilename(null)).toBeNull()
    expect(parseContentDispositionFilename('')).toBeNull()
  })
  it('is null for a header with no filename', () => {
    expect(parseContentDispositionFilename('attachment')).toBeNull()
    expect(parseContentDispositionFilename('inline')).toBeNull()
  })
})
