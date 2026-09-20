/**
 * The Reports screen's single date-range control (spec §6.8): one range
 * drives whichever tab is open, so switching from Daily to Products never
 * silently changes the window being looked at.
 *
 * The pure preset math lives in `components/invoices/dateRange.ts`
 * (`REPORT_DATE_PRESETS`, `presetRange`, `matchPreset`) and is unit-tested
 * there; this hook only wires it to the server's `day_boundary_hour`
 * setting and to component state, the same shape `InvoiceTable` uses for
 * its own range. `ready` stays false until settings have loaded once, so
 * the first report query fires against the real business-day boundary
 * rather than the compile-time default of 0.
 */
import { useEffect, useState } from 'react'
import { useSettings } from '@/components/settings/useSettings'
import {
  type DatePreset,
  type DateRange,
  matchPreset,
  presetRange,
  rangeInverted,
  rangeParams,
  REPORT_DATE_PRESETS,
} from '@/components/invoices/dateRange'

/** Flattens `from`/`to` onto the hook's own return rather than nesting them
 * under a `range` field, so call sites read `range.from`, not the
 * `range.range.from` stutter that nesting would produce. */
export interface UseReportRange extends DateRange {
  preset: DatePreset
  /** True once the range has been set from the real boundary hour. Queries
   * should stay disabled until this is true. */
  ready: boolean
  /** True when `from` is after `to` — the server would answer `invalid_range`. */
  inverted: boolean
  /** `{ from, to }` with blank ends dropped, ready to spread into a query. */
  params: { from?: string; to?: string }
  setRange: (next: DateRange) => void
  setPreset: (preset: DatePreset) => void
}

export function useReportRange(): UseReportRange {
  const { data: settings } = useSettings()
  const boundaryHour = settings?.day_boundary_hour ?? 0

  const [range, setRange] = useState<DateRange>({ from: '', to: '' })
  const [ready, setReady] = useState(false)

  // Lands on "today" once settings (and the real boundary hour) arrive,
  // same as InvoiceTable's own range does.
  useEffect(() => {
    if (ready || !settings) return
    const today = presetRange('today', new Date(), settings.day_boundary_hour)
    if (today) setRange(today)
    setReady(true)
  }, [settings, ready])

  const preset = matchPreset(range, new Date(), boundaryHour, REPORT_DATE_PRESETS)

  const setPreset = (next: DatePreset) => {
    const computed = presetRange(next, new Date(), boundaryHour)
    if (computed) setRange(computed)
  }

  return {
    ...range,
    preset,
    ready,
    inverted: rangeInverted(range),
    params: rangeParams(range),
    setRange,
    setPreset,
  }
}
