/**
 * The generic table every report tab renders (spec §6.8): a fixed set of
 * `{key, label, align, format}` columns, a totals row when the report has
 * one, and an empty state instead of a bare header when the window has no
 * activity. Per-report column lists and totals-row builders live in
 * `reportColumns.tsx`; this file owns only the rendering.
 *
 * `totals` is a lookup by column key rather than a `PeriodSummary` — the
 * shape differs per report (Cashiers has no `PeriodSummary.average`, say),
 * so each report's own `*Totals()` builder in `reportColumns.tsx` maps its
 * columns onto the one `PeriodSummary` the window's totals actually is
 * (backend/internal/reports/summary.go). Passing `null`/`undefined` hides
 * the row outright — receivables and day closes never have one (a balance
 * as of a date and a set of sealed rows are not a period to sum).
 */
import type { ReactNode } from 'react'
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'

export interface ReportColumn<T> {
  key: string
  label: string
  align?: 'left' | 'right'
  format: (row: T) => ReactNode
}

interface Props<T> {
  columns: ReportColumn<T>[]
  rows: T[]
  rowKey: (row: T, index: number) => string
  totals?: Record<string, ReactNode> | null
  emptyMessage?: string
  onRowClick?: (row: T) => void
}

export function ReportTable<T>({
  columns,
  rows,
  rowKey,
  totals,
  emptyMessage = 'No data for this range.',
  onRowClick,
}: Props<T>) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          {columns.map((c) => (
            <TableHead key={c.key} className={c.align === 'right' ? 'text-right' : undefined}>
              {c.label}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.length === 0 && (
          <TableRow>
            <TableCell colSpan={columns.length} className="text-center text-muted-foreground">
              {emptyMessage}
            </TableCell>
          </TableRow>
        )}
        {rows.map((row, i) => (
          <TableRow
            key={rowKey(row, i)}
            className={cn(onRowClick && 'cursor-pointer')}
            onClick={onRowClick ? () => onRowClick(row) : undefined}
          >
            {columns.map((c) => (
              <TableCell key={c.key} className={cn('tabular-nums', c.align === 'right' && 'text-right')}>
                {c.format(row)}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
      {/* rows.length > 0 on purpose: a zeroed totals row under an empty
          state row would read as "nothing sold, and here are its figures",
          which is a contradiction, not a summary. */}
      {totals && rows.length > 0 && (
        <TableFooter>
          <TableRow>
            {columns.map((c) => (
              <TableCell key={c.key} className={cn('font-semibold tabular-nums', c.align === 'right' && 'text-right')}>
                {totals[c.key] ?? ''}
              </TableCell>
            ))}
          </TableRow>
        </TableFooter>
      )}
    </Table>
  )
}
