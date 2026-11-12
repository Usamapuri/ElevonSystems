/**
 * Contract: the sale-date calendar's "today" comes from the business-date
 * arithmetic (businessDateKey), never from the open business_days row.
 *
 * With settings.day_close_enabled = false the day of the LAST sale stays
 * 'open' until the next sale seals it (dayops.autoSeal). A till that read
 * "today" off that row capped the calendar at the last sale's date — on
 * 2026-11-12 both stores could not date a sale after 2026-10-13
 * (audit/BACKDATE_CALENDAR_STALE_DAY_2026-11-12.md). Same style as the
 * backend's grep contract tests: read the source, refuse the pattern.
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const posSource = readFileSync(resolve(__dirname, '../../routes/_app/pos.tsx'), 'utf8')

describe('sale-date calendar contract', () => {
  it('derives todayKey from businessDateKey, not from the open day row', () => {
    const line = posSource.split('\n').find((l) => /const todayKey\s*=/.test(l))
    expect(line, 'pos.tsx must still define todayKey').toBeDefined()
    expect(line).toContain('businessDateKey(')
    expect(line).not.toContain('dayDateKey(')
    expect(line).not.toContain('dayCurrent')
  })

  it('does not read the open day date anywhere in the till route', () => {
    expect(posSource).not.toMatch(/\bdayDateKey\b/)
  })
})
