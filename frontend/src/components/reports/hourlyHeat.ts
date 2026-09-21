/**
 * Pure derivations for the Hourly report's heatmap (Task P8 — the owner
 * asked for a heatmap after seeing the 24-row table; spec §6.8). Kept out of
 * `HourlyHeatmap.tsx` so the shading and row-collapse rules are
 * unit-testable without mounting React (this repo's vitest runs in the node
 * environment — logic tests only, no component rendering).
 *
 * Color, per the dataviz skill (`color-formula.md`): a heatmap cell encodes
 * magnitude, so it is a **sequential** ramp — one hue (the app's own
 * `--primary` safety orange), light→dark, never a rainbow. Zero is not the
 * ramp's lightest step; it carries no fill at all, so "nothing sold" reads
 * as absence rather than as the bottom of the scale. The five non-zero steps
 * are quantile bins of the window's own nonzero cells, not fixed rupee
 * bands — a quiet store's Rs 200 hour and a busy one's Rs 20,000 hour should
 * each still show contrast across their own 24×7 grid.
 */
import type { HeatCell } from '@/types'
import { hourLabel } from './reportColumns'

/** Weekday labels, Monday-first — `HeatCell.weekday` is 0=Mon…6=Sun
 * (backend: EXTRACT(ISODOW …) − 1), so index straight into this array. */
export const WEEKDAY_LABELS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] as const

/** Number of non-zero shading steps in the sequential ramp. A cell's `step`
 * is 0 (no sales, no fill) through HEAT_STEPS (the darkest, busiest bin). */
export const HEAT_STEPS = 5

/** `hsl(var(--primary) / alpha)` opacity for each non-zero step, index 0 =
 * step 1 (lightest) … index HEAT_STEPS-1 = step HEAT_STEPS (darkest). One
 * hue, monotonically increasing — the sequential ramp the dataviz skill
 * calls for; the validator script only judges categorical/ordinal palettes,
 * so a monotone single-hue alpha ramp like this one is checked by
 * construction (color-formula.md's "Scope" note) rather than by the script. */
export const STEP_ALPHAS = [0.14, 0.28, 0.46, 0.66, 0.88] as const

/** A cell's on-cell rupee label only earns the ink past this step — the
 * lighter two-thirds of the ramp rely on the tooltip, the legend and the
 * table view instead (marks-and-anatomy.md: "label selectively, never a
 * number on every point"). */
export const LABEL_MIN_STEP = 3

/** The hours collapsed into a single "Night" row when every one of them is
 * silent across the whole window. */
const NIGHT_HOURS: number[] = [0, 1, 2, 3, 4, 5]

/**
 * Quantile thresholds for the sequential ramp, computed over the window's
 * own nonzero values only — computing quantiles over all 168 mostly-zero
 * cells would crush every real sale into the bottom step on a quiet day.
 *
 * Returns `steps - 1` ascending thresholds. A value at or below
 * `thresholds[i]` belongs to step `i + 1`; anything above the last
 * threshold is step `steps`. Values <= 0 are never consulted against these
 * (they are always step 0 — see `cellShade`), and an all-zero or empty input
 * returns no thresholds at all.
 */
export function quantileSteps(values: number[], steps: number = HEAT_STEPS): number[] {
  const nonZero = values.filter((v) => v > 0).sort((a, b) => a - b)
  if (nonZero.length === 0 || steps < 2) return []
  const thresholds: number[] = []
  for (let i = 1; i < steps; i++) {
    const idx = Math.min(nonZero.length - 1, Math.max(0, Math.ceil((nonZero.length * i) / steps) - 1))
    thresholds.push(nonZero[idx])
  }
  return thresholds
}

/** Which shading step (0..thresholds.length+1) a cell's value falls into.
 * 0 is always "no sales" — the value is never compared against the
 * thresholds once it is <= 0, so a cell with no activity never picks up a
 * fill by rounding into the bottom bin. */
export function cellShade(value: number, thresholds: number[]): number {
  if (!(value > 0)) return 0
  for (let i = 0; i < thresholds.length; i++) {
    if (value <= thresholds[i]) return i + 1
  }
  return thresholds.length + 1
}

/** One row of the rendered grid: a label and the hour(s) of day it covers —
 * one hour normally, or all six night hours when they collapse. */
export interface HeatRowPlan {
  label: string
  hours: number[]
}

/**
 * The heatmap's row plan: one row per hour 00–23, unless every weekday's
 * 00:00–05:59 is entirely silent across the whole window, in which case
 * those six hours collapse into a single "Night" row. Six rows of identical
 * empty cells tell the owner nothing that one "Night" row summing to zero
 * does not already say, and a store that never opens before dawn should not
 * have a quarter of its heatmap be dead weight.
 */
export function heatRowPlan(cells: HeatCell[]): HeatRowPlan[] {
  const nightIsSilent = cells.every((c) => !NIGHT_HOURS.includes(c.hour) || c.invoices === 0)
  const rows: HeatRowPlan[] = []
  if (nightIsSilent) {
    rows.push({ label: 'Night', hours: [...NIGHT_HOURS] })
    for (let h = 6; h < 24; h++) rows.push({ label: hourLabel(h), hours: [h] })
  } else {
    for (let h = 0; h < 24; h++) rows.push({ label: hourLabel(h), hours: [h] })
  }
  return rows
}

/** Sums one weekday's cells across a set of hours — the aggregation a
 * multi-hour row (the collapsed "Night" row) needs. Exposed separately from
 * `buildHeatGrid` so the summing itself is directly testable: `heatRowPlan`
 * only ever collapses hours that are silent on every weekday, so summing
 * *nonzero* data through that specific path can never be exercised
 * end-to-end — this function is what actually gets tested with real numbers. */
export function sumCellsForRow(cells: HeatCell[], weekday: number, hours: number[]): { invoices: number; net: number } {
  let invoices = 0
  let net = 0
  for (const c of cells) {
    if (c.weekday === weekday && hours.includes(c.hour)) {
      invoices += c.invoices
      net += c.net
    }
  }
  return { invoices, net }
}

/** One rendered grid cell: which weekday/row it sits in, its aggregated
 * totals (summed across `hours` when a row spans more than one, i.e. the
 * collapsed Night row) and its shading step. */
export interface HeatGridCell {
  weekday: number
  hours: number[]
  invoices: number
  net: number
  step: number
}

export interface HeatGridRow {
  label: string
  hours: number[]
  cells: HeatGridCell[] // length 7, index = weekday
}

export interface HeatGrid {
  rows: HeatGridRow[]
  /** Ascending step thresholds, for the legend — see `quantileSteps`. */
  thresholds: number[]
}

/** Builds the full renderable grid from the raw 168-cell API response: the
 * row plan (with or without the Night collapse), each row's seven weekday
 * totals, and the quantile thresholds each cell's `step` was shaded against. */
export function buildHeatGrid(cells: HeatCell[]): HeatGrid {
  const plan = heatRowPlan(cells)
  const rows: HeatGridRow[] = plan.map(({ label, hours }) => {
    const rowCells: HeatGridCell[] = []
    for (let weekday = 0; weekday < 7; weekday++) {
      const { invoices, net } = sumCellsForRow(cells, weekday, hours)
      rowCells.push({ weekday, hours, invoices, net, step: 0 })
    }
    return { label, hours, cells: rowCells }
  })

  const thresholds = quantileSteps(rows.flatMap((r) => r.cells.map((c) => c.net)))
  for (const row of rows) {
    for (const cell of row.cells) cell.step = cellShade(cell.net, thresholds)
  }

  return { rows, thresholds }
}

// ── roving tabindex (WAI-ARIA APG grid pattern) ─────────────────────────────

/** A grid position by index — `row` into `HeatGrid.rows`, `col` the weekday
 * (0=Mon…6=Sun, same as `HeatCell.weekday`). */
export interface CellPos {
  row: number
  col: number
}

const GRID_NAV_KEYS = new Set(['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Home', 'End'])

/** Whether a key is one `nextCellPos` handles — the component's keydown
 * handler uses this to decide when to `preventDefault()` (arrow/Home/End
 * would otherwise scroll the page) and let every other key, notably Tab,
 * behave exactly as the browser's own default. */
export function isGridNavKey(key: string): boolean {
  return GRID_NAV_KEYS.has(key)
}

/**
 * The next active cell for the heatmap's roving-tabindex navigation, given
 * the currently active cell, a keyboard key, and the grid's bounds. Pure so
 * the navigation math is unit-testable without mounting the grid (this
 * repo's vitest is node-only, no DOM tests) — `HourlyHeatmap.tsx` only wires
 * this to real DOM focus.
 *
 * Movement is spatial, per the WAI-ARIA APG grid pattern and how the grid
 * actually reads on screen (columns are weekdays, rows are hours — spec
 * §6.8, the brief): ArrowLeft/Right move across columns (weekdays) within
 * the same hour row; ArrowUp/Down move across rows (hours) within the same
 * weekday column; Home/End jump to the first/last column of the current row
 * — the row's own ends, not the whole grid's. Every move clamps to the
 * grid's bounds rather than wrapping, so repeated presses at an edge are a
 * no-op instead of jumping to the opposite side. Any other key returns the
 * position unchanged.
 */
export function nextCellPos(pos: CellPos, key: string, rowCount: number, colCount: number): CellPos {
  const { row, col } = pos
  switch (key) {
    case 'ArrowUp':
      return { row: Math.max(0, row - 1), col }
    case 'ArrowDown':
      return { row: Math.min(rowCount - 1, row + 1), col }
    case 'ArrowLeft':
      return { row, col: Math.max(0, col - 1) }
    case 'ArrowRight':
      return { row, col: Math.min(colCount - 1, col + 1) }
    case 'Home':
      return { row, col: 0 }
    case 'End':
      return { row, col: colCount - 1 }
    default:
      return pos
  }
}
