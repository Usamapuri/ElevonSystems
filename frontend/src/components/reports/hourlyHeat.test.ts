import { describe, expect, it } from 'vitest'
import type { HeatCell } from '@/types'
import {
  HEAT_STEPS, STEP_ALPHAS, WEEKDAY_LABELS, buildHeatGrid, cellShade, heatRowPlan, isGridNavKey,
  legendBands, nextCellPos, quantileSteps, sumCellsForRow,
} from './hourlyHeat'

/** All 168 zero cells, with a few overridden — the same shape the backend
 * always returns (zero-filled, weekday-major), so a test only has to name
 * the cells it cares about. */
function makeCells(overrides: Partial<HeatCell>[] = []): HeatCell[] {
  const cells: HeatCell[] = []
  for (let weekday = 0; weekday < 7; weekday++) {
    for (let hour = 0; hour < 24; hour++) {
      cells.push({ weekday, hour, invoices: 0, net: 0 })
    }
  }
  for (const o of overrides) {
    const i = cells.findIndex((c) => c.weekday === o.weekday && c.hour === o.hour)
    if (i === -1) throw new Error(`no cell for weekday=${o.weekday} hour=${o.hour}`)
    cells[i] = { ...cells[i], ...o }
  }
  return cells
}

describe('WEEKDAY_LABELS', () => {
  it('is Monday-first, matching HeatCell.weekday 0=Mon…6=Sun', () => {
    expect(WEEKDAY_LABELS).toEqual(['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'])
  })
})

describe('quantileSteps', () => {
  it('returns no thresholds for an empty or all-zero window', () => {
    expect(quantileSteps([])).toEqual([])
    expect(quantileSteps([0, 0, 0])).toEqual([])
  })

  it('ignores zeros when computing quantiles', () => {
    // Only the two nonzero values should shape the thresholds; a sea of
    // zeros must not crush them into one bin.
    const a = quantileSteps([0, 0, 0, 0, 100, 200], 2)
    const b = quantileSteps([100, 200], 2)
    expect(a).toEqual(b)
  })

  it('returns steps - 1 ascending thresholds', () => {
    const values = Array.from({ length: 20 }, (_, i) => (i + 1) * 100) // 100..2000
    const thresholds = quantileSteps(values, HEAT_STEPS)
    expect(thresholds).toHaveLength(HEAT_STEPS - 1)
    for (let i = 1; i < thresholds.length; i++) {
      expect(thresholds[i]).toBeGreaterThanOrEqual(thresholds[i - 1])
    }
  })

  it('a single nonzero value produces a flat ramp that still classifies it', () => {
    const thresholds = quantileSteps([500])
    // Every threshold collapses to the one value present — cellShade must
    // still place it in a valid, nonzero step rather than throwing or
    // falling through to 0 (which would misread it as "no sales").
    const step = cellShade(500, thresholds)
    expect(step).toBeGreaterThanOrEqual(1)
    expect(step).toBeLessThanOrEqual(HEAT_STEPS)
  })
})

describe('legendBands', () => {
  it('draws one band per ramp step for a spread window', () => {
    const bands = legendBands([100, 200, 300, 400])
    expect(bands).toHaveLength(HEAT_STEPS)
    expect(bands[0]).toEqual({ alpha: STEP_ALPHAS[0], from: 0, to: 100 })
    expect(bands[1]).toEqual({ alpha: STEP_ALPHAS[1], from: 100, to: 200 })
    expect(bands[HEAT_STEPS - 1]).toEqual({ alpha: STEP_ALPHAS[HEAT_STEPS - 1], from: 400, to: null })
  })

  it('collapses to a single band when every threshold is the same value', () => {
    // One busy cell in the window: quantileSteps returns [v, v, v, v], which
    // would otherwise render "Up to v · v–v · v–v · Over v".
    const thresholds = quantileSteps([500])
    expect(new Set(thresholds).size).toBe(1)
    const bands = legendBands(thresholds)
    expect(bands).toHaveLength(1)
    expect(bands[0]).toEqual({ alpha: STEP_ALPHAS[0], from: 0, to: 500 })
    // And that one band is the step the grid actually paints for that cell.
    expect(cellShade(500, thresholds)).toBe(1)
  })

  it('draws nothing when the window has no sales at all', () => {
    expect(legendBands(quantileSteps([0, 0, 0]))).toEqual([])
  })
})

describe('cellShade', () => {
  const thresholds = [100, 200, 300, 400] // HEAT_STEPS - 1

  it('shades zero and negative values as step 0 (no fill), never touching the thresholds', () => {
    expect(cellShade(0, thresholds)).toBe(0)
    expect(cellShade(-5, thresholds)).toBe(0)
  })

  it('classifies values into ascending steps by threshold', () => {
    expect(cellShade(1, thresholds)).toBe(1)
    expect(cellShade(100, thresholds)).toBe(1)
    expect(cellShade(101, thresholds)).toBe(2)
    expect(cellShade(400, thresholds)).toBe(4)
    expect(cellShade(401, thresholds)).toBe(5) // past the last threshold — the darkest step
  })

  it('never returns more than HEAT_STEPS for a HEAT_STEPS-sized threshold list', () => {
    expect(cellShade(1_000_000, thresholds)).toBe(HEAT_STEPS)
  })
})

describe('heatRowPlan', () => {
  it('collapses 00:00–05:59 into one Night row when every weekday is silent then', () => {
    const cells = makeCells([{ weekday: 1, hour: 10, invoices: 1, net: 500 }])
    const rows = heatRowPlan(cells)
    expect(rows[0]).toEqual({ label: 'Night', hours: [0, 1, 2, 3, 4, 5] })
    // 1 Night row + hours 6..23 (18 rows) = 19 rows total.
    expect(rows).toHaveLength(19)
    expect(rows[rows.length - 1]).toEqual({ label: '23:00', hours: [23] })
  })

  it('keeps all 24 individual hours when any night hour has activity', () => {
    const cells = makeCells([{ weekday: 3, hour: 4, invoices: 1, net: 472 }])
    const rows = heatRowPlan(cells)
    expect(rows).toHaveLength(24)
    expect(rows[0]).toEqual({ label: '00:00', hours: [0] })
    expect(rows[4]).toEqual({ label: '04:00', hours: [4] })
  })

  it('collapses Night on a fully empty grid', () => {
    expect(heatRowPlan(makeCells())).toHaveLength(19)
  })
})

describe('sumCellsForRow', () => {
  it('sums one weekday across several hours', () => {
    const cells = makeCells([
      { weekday: 2, hour: 1, invoices: 1, net: 300 },
      { weekday: 2, hour: 3, invoices: 2, net: 700 },
      { weekday: 2, hour: 10, invoices: 5, net: 9999 }, // outside the range — must not be counted
    ])
    expect(sumCellsForRow(cells, 2, [0, 1, 2, 3, 4, 5])).toEqual({ invoices: 3, net: 1000 })
  })

  it('sums to zero for a weekday/hours with no matching cells', () => {
    expect(sumCellsForRow(makeCells(), 5, [6])).toEqual({ invoices: 0, net: 0 })
  })
})

describe('buildHeatGrid', () => {
  it('collapses to a Night row of all zeros when the window has no night activity anywhere', () => {
    const cells = makeCells([{ weekday: 1, hour: 10, invoices: 1, net: 500 }])
    const grid = buildHeatGrid(cells)
    const nightRow = grid.rows.find((r) => r.label === 'Night')
    expect(nightRow).toBeDefined()
    expect(nightRow!.cells.every((c) => c.invoices === 0 && c.net === 0 && c.step === 0)).toBe(true)
  })

  it('every non-collapsed grid cell carries the same total as its source HeatCell', () => {
    const cells = makeCells([
      { weekday: 1, hour: 10, invoices: 1, net: 1180 },
      { weekday: 2, hour: 11, invoices: 1, net: 105 },
    ])
    const grid = buildHeatGrid(cells)
    const row10 = grid.rows.find((r) => r.label === '10:00')!
    expect(row10.cells[1]).toMatchObject({ invoices: 1, net: 1180 })
    const row11 = grid.rows.find((r) => r.label === '11:00')!
    expect(row11.cells[2]).toMatchObject({ invoices: 1, net: 105 })
  })

  it('shades every nonzero cell above step 0 and every zero cell at exactly step 0', () => {
    const cells = makeCells([
      { weekday: 0, hour: 9, invoices: 1, net: 100 },
      { weekday: 4, hour: 18, invoices: 3, net: 9000 },
    ])
    const grid = buildHeatGrid(cells)
    for (const row of grid.rows) {
      for (const cell of row.cells) {
        if (cell.net > 0) expect(cell.step).toBeGreaterThan(0)
        else expect(cell.step).toBe(0)
      }
    }
  })

  it('an all-zero window produces a grid with every cell at step 0 and no thresholds', () => {
    const grid = buildHeatGrid(makeCells())
    expect(grid.thresholds).toEqual([])
    expect(grid.rows.every((r) => r.cells.every((c) => c.step === 0))).toBe(true)
  })
})

describe('isGridNavKey', () => {
  it('recognizes the six APG grid navigation keys', () => {
    for (const key of ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Home', 'End']) {
      expect(isGridNavKey(key)).toBe(true)
    }
  })

  it('rejects every other key, including Tab and Enter', () => {
    for (const key of ['Tab', 'Enter', ' ', 'Escape', 'a', 'PageUp']) {
      expect(isGridNavKey(key)).toBe(false)
    }
  })
})

describe('nextCellPos', () => {
  // A grid 19 rows tall (the Night-collapsed shape) x 7 columns wide (Mon…Sun).
  const rowCount = 19
  const colCount = 7

  it('moves right and left across columns (weekdays), within the same row', () => {
    expect(nextCellPos({ row: 5, col: 2 }, 'ArrowRight', rowCount, colCount)).toEqual({ row: 5, col: 3 })
    expect(nextCellPos({ row: 5, col: 2 }, 'ArrowLeft', rowCount, colCount)).toEqual({ row: 5, col: 1 })
  })

  it('moves up and down across rows (hours), within the same column', () => {
    expect(nextCellPos({ row: 5, col: 2 }, 'ArrowDown', rowCount, colCount)).toEqual({ row: 6, col: 2 })
    expect(nextCellPos({ row: 5, col: 2 }, 'ArrowUp', rowCount, colCount)).toEqual({ row: 4, col: 2 })
  })

  it('clamps at the edges instead of wrapping', () => {
    expect(nextCellPos({ row: 0, col: 0 }, 'ArrowUp', rowCount, colCount)).toEqual({ row: 0, col: 0 })
    expect(nextCellPos({ row: 0, col: 0 }, 'ArrowLeft', rowCount, colCount)).toEqual({ row: 0, col: 0 })
    expect(nextCellPos({ row: rowCount - 1, col: 6 }, 'ArrowDown', rowCount, colCount)).toEqual({ row: rowCount - 1, col: 6 })
    expect(nextCellPos({ row: rowCount - 1, col: 6 }, 'ArrowRight', rowCount, colCount)).toEqual({ row: rowCount - 1, col: 6 })
  })

  it('Home and End jump to the current row\'s first and last column, not the whole grid\'s', () => {
    expect(nextCellPos({ row: 8, col: 4 }, 'Home', rowCount, colCount)).toEqual({ row: 8, col: 0 })
    expect(nextCellPos({ row: 8, col: 4 }, 'End', rowCount, colCount)).toEqual({ row: 8, col: 6 })
  })

  it('leaves the position unchanged for a key it does not handle', () => {
    expect(nextCellPos({ row: 3, col: 3 }, 'Tab', rowCount, colCount)).toEqual({ row: 3, col: 3 })
    expect(nextCellPos({ row: 3, col: 3 }, 'Enter', rowCount, colCount)).toEqual({ row: 3, col: 3 })
  })
})
