/**
 * Hour × weekday heatmap for the Hourly report tab (spec §6.8; Task P8 — the
 * owner, looking at the plain 24-row table, asked "isn't that supposed to be
 * a heatmap?"). One cell per (weekday, hour) bucket in the business
 * timezone; the flat 24-row table stays available behind a "Show table"
 * disclosure for anyone who wants exact figures without hovering 168 cells.
 *
 * Color, per the dataviz skill: a **sequential** ramp — one hue (the app's
 * own `--primary` safety orange) at five increasing `hsl(var(--primary) /
 * alpha)` steps, quantile-binned over the window's own nonzero cells
 * (`hourlyHeat.ts`). Zero cells carry no fill — the card surface shows
 * through, so "nothing sold" reads as absence rather than the ramp's palest
 * step. The sequential-ramp check in `dataviz`'s validator script only
 * judges categorical/ordinal palettes (`color-formula.md`'s "Scope" note);
 * a monotone single-hue alpha ramp satisfies the sequential rule ("one hue,
 * light→dark") by construction.
 *
 * Every cell is its own tooltip trigger (`ui/tooltip.tsx`, Radix) so the
 * same content shows on hover and on keyboard focus — `interaction.md`:
 * "Same details on keyboard focus as on hover." Cells past `LABEL_MIN_STEP`
 * also carry the net figure directly, in the app's `--primary-foreground`
 * navy ink once the fill is dark enough to need it (`marks-and-anatomy.md`:
 * "pick white or ink by the fill's luminance"); lighter cells rely on the
 * tooltip, the legend and the table view (`marks-and-anatomy.md`: "label
 * selectively, never a number on every point").
 *
 * Keyboard: `role="grid"` commits to the WAI-ARIA APG roving-tabindex
 * pattern, not 168 independent Tab stops — exactly one cell (the "active"
 * one) is ever `tabIndex={0}`; every other cell is `-1`. Tab reaches the
 * grid once and, with the same keystroke, leaves it for the next control
 * (the "Show table" button). Arrow keys move the active cell spatially
 * (`nextCellPos`, `hourlyHeat.ts`); real DOM focus moves with it, via refs,
 * because the tooltip needs actual focus to open per the rule above.
 */
import { useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ReportTable } from './ReportTable'
import { hourlyColumns, hourlyTotals } from './reportColumns'
import {
  HEAT_STEPS, LABEL_MIN_STEP, STEP_ALPHAS, WEEKDAY_LABELS, buildHeatGrid, isGridNavKey, legendBands,
  nextCellPos,
} from './hourlyHeat'
import { compactMoney, formatMoney } from '@/lib/money'
import { cn } from '@/lib/utils'
import type { HeatCell, HourRow, PeriodSummary } from '@/types'

interface Props {
  cells: HeatCell[]
  rows: HourRow[]
  totals: PeriodSummary | null
}

const WEEKDAY_COUNT = WEEKDAY_LABELS.length

/** The rendered fill for one shading step — 0 is deliberately `transparent`,
 * not the ramp's lightest opaque step; see the file header. */
function cellBackground(step: number): string {
  return step === 0 ? 'transparent' : `hsl(var(--primary) / ${STEP_ALPHAS[step - 1]})`
}

/** Once a cell's fill is dark enough (the top two steps) its on-cell label
 * needs the same navy-on-orange pairing the rest of the app already uses
 * for text sitting inside the primary color; lighter fills stay on the
 * ordinary foreground token. */
function cellTextClass(step: number): string {
  return step >= HEAT_STEPS - 1 ? 'text-primary-foreground' : 'text-foreground'
}

export function HourlyHeatmap({ cells, rows, totals }: Props) {
  const [showTable, setShowTable] = useState(false)
  const grid = useMemo(() => buildHeatGrid(cells), [cells])
  const hasSales = useMemo(() => grid.rows.some((row) => row.cells.some((c) => c.net > 0)), [grid])
  const rowCount = grid.rows.length

  // The one active (tabIndex=0) cell. Clamped on read, not reset in an
  // effect: a date-range change can shrink rowCount (the Night collapse
  // toggling) without this component unmounting, and clamping here is
  // enough to keep `active` inside the new grid rather than pointing past
  // its last row.
  const [active, setActive] = useState<{ row: number; col: number }>({ row: 0, col: 0 })
  const activeRow = Math.min(active.row, Math.max(0, rowCount - 1))
  const activeCol = Math.min(active.col, WEEKDAY_COUNT - 1)

  // Real DOM nodes, keyed "row-col", so an arrow key can move actual focus
  // (not just the roving-tabindex bookkeeping) — the tooltip only opens on
  // real focus.
  const cellRefs = useRef(new Map<string, HTMLDivElement>())

  const handleGridKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!isGridNavKey(e.key)) return
    e.preventDefault() // arrows/Home/End would otherwise scroll the page
    const next = nextCellPos({ row: activeRow, col: activeCol }, e.key, rowCount, WEEKDAY_COUNT)
    cellRefs.current.get(`${next.row}-${next.col}`)?.focus()
  }

  if (!hasSales) {
    return <p className="py-8 text-center text-sm text-muted-foreground">No sales in this range.</p>
  }

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-border bg-card p-3">
        <div
          role="grid"
          aria-label="Invoices and net sales by hour and weekday"
          className="grid min-w-[560px] gap-1"
          style={{ gridTemplateColumns: '52px repeat(7, minmax(0, 1fr))' }}
          onKeyDown={handleGridKeyDown}
        >
          <div role="row" className="contents">
            <div role="columnheader" aria-hidden="true" />
            {WEEKDAY_LABELS.map((label) => (
              <div key={label} role="columnheader" className="pb-1 text-center text-xs font-semibold text-muted-foreground">
                {label}
              </div>
            ))}
          </div>
          {grid.rows.map((row, rowIndex) => (
            <div key={row.label} role="row" className="contents">
              <div role="rowheader" className="flex items-center justify-end pr-2 text-xs tabular text-muted-foreground">
                {row.label}
              </div>
              {row.cells.map((cell) => {
                const weekdayLabel = WEEKDAY_LABELS[cell.weekday]
                const invoiceWord = cell.invoices === 1 ? 'invoice' : 'invoices'
                const tooltipText = `${weekdayLabel} ${row.label} — ${cell.invoices} ${invoiceWord}, ${formatMoney(cell.net)}`
                const isActive = rowIndex === activeRow && cell.weekday === activeCol
                const refKey = `${rowIndex}-${cell.weekday}`
                return (
                  <Tooltip key={cell.weekday}>
                    <TooltipTrigger asChild>
                      <div
                        ref={(el) => {
                          if (el) cellRefs.current.set(refKey, el)
                          else cellRefs.current.delete(refKey)
                        }}
                        role="gridcell"
                        tabIndex={isActive ? 0 : -1}
                        aria-label={tooltipText}
                        onFocus={() => setActive({ row: rowIndex, col: cell.weekday })}
                        className={cn(
                          'flex h-8 items-center justify-center rounded-sm outline-none transition-shadow',
                          'hover:ring-2 hover:ring-primary/60 focus-visible:ring-2 focus-visible:ring-ring',
                        )}
                        style={{ backgroundColor: cellBackground(cell.step) }}
                      >
                        {cell.step >= LABEL_MIN_STEP && (
                          <span className={cn('tabular text-[10px] font-semibold leading-none', cellTextClass(cell.step))}>
                            {compactMoney(cell.net)}
                          </span>
                        )}
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>{tooltipText}</TooltipContent>
                  </Tooltip>
                )
              })}
            </div>
          ))}
        </div>
      </div>

      <HeatmapLegend thresholds={grid.thresholds} />

      <div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="gap-1 text-xs text-muted-foreground"
          aria-expanded={showTable}
          onClick={() => setShowTable((v) => !v)}
        >
          {showTable ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
          {showTable ? 'Hide table' : 'Show table'}
        </Button>
        {showTable && (
          <div className="mt-2">
            <ReportTable
              columns={hourlyColumns}
              rows={rows}
              rowKey={(r) => String(r.hour)}
              totals={totals ? hourlyTotals(totals) : null}
              emptyMessage="No sales in this range."
            />
          </div>
        )}
      </div>
    </div>
  )
}

/** The five-step scale legend (`dataviz`'s components.md: "Scale legend
 * (sequential / diverging)"), plus the zero swatch — the step thresholds
 * are the whole point of a legend on a quantile ramp, since the same shade
 * means a different rupee band on a busy window than on a quiet one.
 * How many bands there are is `legendBands`' decision, not this
 * component's: a window whose thresholds all collapse to one value gets a
 * single band rather than four that read "v–v". */
function HeatmapLegend({ thresholds }: { thresholds: number[] }) {
  const bands = legendBands(thresholds)
  if (bands.length === 0) return null
  return (
    <div role="group" aria-label="Shading legend" className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
      <span className="font-semibold text-foreground">Net sales</span>
      <LegendSwatch color="transparent" label="No sales" bordered />
      {bands.map((band) => {
        const label =
          band.to === null
            ? `Over ${compactMoney(band.from)}`
            : band.from === 0
              ? `Up to ${compactMoney(band.to)}`
              : `${compactMoney(band.from)}–${compactMoney(band.to)}`
        return <LegendSwatch key={band.alpha} color={`hsl(var(--primary) / ${band.alpha})`} label={label} />
      })}
    </div>
  )
}

function LegendSwatch({ color, label, bordered }: { color: string; label: string; bordered?: boolean }) {
  return (
    <span className="flex items-center gap-1.5">
      <span className={cn('h-3.5 w-3.5 rounded-sm', bordered && 'border border-border')} style={{ backgroundColor: color }} />
      {label}
    </span>
  )
}
