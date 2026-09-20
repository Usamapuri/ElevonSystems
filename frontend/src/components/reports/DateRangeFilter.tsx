/**
 * The Reports screen's global filter bar (spec §6.8): preset buttons plus
 * two plain date inputs for a custom range. Ported from RETAIL's
 * `DateRangeFilter` (calendar popover + `date-fns`), but this project has
 * no `Calendar` UI primitive vendored and the invoice browser already
 * proved the simpler pattern — preset buttons next to native `<input
 * type=date>` fields, business dates as bare `YYYY-MM-DD` strings
 * throughout (`InvoiceTable.tsx`). Same pattern here, with the wider
 * `REPORT_DATE_PRESETS` set instead of the invoice browser's three.
 */
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { presetLabel, REPORT_DATE_PRESETS } from '@/components/invoices/dateRange'
import type { UseReportRange } from './useReportRange'

interface Props {
  range: UseReportRange
}

export function DateRangeFilter({ range }: Props) {
  return (
    <div className="flex flex-wrap items-end gap-3">
      <div className="flex flex-wrap gap-2">
        {REPORT_DATE_PRESETS.map((preset) => (
          <Button
            key={preset}
            type="button"
            size="sm"
            variant={range.preset === preset ? 'default' : 'outline'}
            onClick={() => range.setPreset(preset)}
          >
            {presetLabel(preset)}
          </Button>
        ))}
      </div>

      <div className="space-y-1">
        <Label htmlFor="reports-from" className="text-xs text-muted-foreground">
          From
        </Label>
        <Input
          id="reports-from"
          type="date"
          className="w-[10.5rem]"
          value={range.from}
          onChange={(e) => range.setRange({ from: e.target.value, to: range.to })}
        />
      </div>
      <div className="space-y-1">
        <Label htmlFor="reports-to" className="text-xs text-muted-foreground">
          To
        </Label>
        <Input
          id="reports-to"
          type="date"
          className="w-[10.5rem]"
          value={range.to}
          onChange={(e) => range.setRange({ from: range.from, to: e.target.value })}
        />
      </div>

      {range.inverted && (
        <p className="text-sm text-destructive">The From date is after the To date — no report can match that.</p>
      )}
    </div>
  )
}
