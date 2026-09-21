/**
 * The Z-report on screen — the same figures the thermal slip prints, in the
 * same order, so the owner can check the screen against the paper without
 * translating between two layouts.
 *
 * Two rules carried over from the print builder (lib/print/zReport.ts):
 *
 *  1. Where the day is sealed, the tender table shows the SEALED `expected_*`
 *     from the row, not the live recomputation. They should be identical;
 *     when they are not, something changed after the seal, and a screen that
 *     silently switched to the live number would hide exactly the fact the
 *     owner needs to see.
 *  2. A nullable figure renders as an em dash, never as 0 and never as NaN.
 *     On an open day nothing has been counted, and "0" there reads as
 *     "counted, and it was empty" — a different and much worse claim.
 */
import type { ReactNode } from 'react'
import { Printer } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatMoney } from '@/lib/money'
import { formatBusinessDate, formatDateTimePK, formatTimePK } from '@/lib/print/format'
import { cn } from '@/lib/utils'
import type { AppSettings, ZReport } from '@/types'

const DASH = '—'

/** A nullable money figure. Null is "nobody counted", not zero. */
function money(n: number | null | undefined): string {
  return typeof n === 'number' && Number.isFinite(n) ? formatMoney(n) : DASH
}

/** A nullable signed money figure, for the variance column. */
function signedMoney(n: number | null | undefined): string {
  if (typeof n !== 'number' || !Number.isFinite(n)) return DASH
  if (n === 0) return formatMoney(0)
  return (n > 0 ? '+' : '-') + formatMoney(Math.abs(n))
}

/** A nullable count. */
function count(n: number | null | undefined): string {
  return typeof n === 'number' && Number.isFinite(n) ? String(n) : DASH
}

function who(name: string | null, at: string | null): string {
  if (!at) return DASH
  return `${name ?? DASH} · ${formatTimePK(at)}`
}

interface Props {
  z: ZReport
  /** Print needs paper width and the business identity; without settings the
   * button stays off rather than printing a slip with invented blanks. */
  settings: AppSettings | undefined
  /** Reopen / force close live here on the day-close screen; the reports
   * screen will pass nothing. */
  actions?: ReactNode
  onPrint: () => void
  printing?: boolean
}

export function ZReportView({ z, settings, actions, onPrint, printing = false }: Props) {
  const day = z.day
  const expected = z.expected
  const sealed = day.status === 'closed'

  const rows: { label: string; expected: number; counted: number | null; variance: number | null }[] = [
    { label: 'Cash', expected: day.expected_cash ?? expected.cash, counted: day.counted_cash, variance: day.cash_variance },
    { label: 'Card', expected: day.expected_card ?? expected.card, counted: day.counted_card, variance: day.card_variance },
    {
      label: 'Online',
      expected: day.expected_online ?? expected.online,
      counted: day.counted_online,
      variance: day.online_variance,
    },
  ]

  return (
    <Card>
      <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:space-y-0">
        <div>
          <CardTitle className="flex flex-wrap items-center gap-2">
            {sealed ? 'Z-report' : 'X-read (day open)'}
            <Badge variant={sealed ? 'secondary' : 'success'} className="uppercase">
              {day.status}
            </Badge>
          </CardTitle>
          <CardDescription>
            {formatBusinessDate(day.business_date)} · printed figures as of {formatDateTimePK(z.generated_at)}
          </CardDescription>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {actions}
          <Button variant="outline" onClick={onPrint} disabled={!settings || printing}>
            <Printer className="mr-2 h-4 w-4" /> Print
          </Button>
        </div>
      </CardHeader>

      <CardContent className="space-y-6">
        <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-4">
          <Figure label="Opened by" value={who(day.opened_by_name, day.opened_at)} />
          <Figure label="Closed by" value={sealed || day.closed_at ? who(day.closed_by_name, day.closed_at) : 'not closed'} />
          <Figure label="Opening cash" value={formatMoney(day.opening_cash)} />
          <Figure label="Invoices" value={`${expected.invoice_count} (${expected.void_count} voided)`} />
        </div>

        <section>
          <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground">Tender</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tender</TableHead>
                <TableHead className="text-right">Expected</TableHead>
                <TableHead className="text-right">Counted</TableHead>
                <TableHead className="text-right">Variance</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.label}>
                  <TableCell className="font-medium">{row.label}</TableCell>
                  <TableCell className="text-right tabular-nums">{money(row.expected)}</TableCell>
                  <TableCell className="text-right tabular-nums">{money(row.counted)}</TableCell>
                  <TableCell
                    className={cn(
                      'text-right tabular-nums',
                      typeof row.variance === 'number' && row.variance !== 0 && 'font-medium text-warning-ink',
                    )}
                  >
                    {signedMoney(row.variance)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!sealed && (
            <p className="mt-2 text-xs text-muted-foreground">
              Nothing is counted until the day is closed, so those columns read {DASH} rather than zero.
            </p>
          )}
        </section>

        <div className="grid gap-6 md:grid-cols-2">
          <section>
            <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground">Sales</h3>
            <dl className="space-y-1 text-sm">
              <Line label="Gross sales" value={formatMoney(expected.gross_sales)} />
              <Line label="Discounts" value={`-${formatMoney(expected.discounts)}`} />
              <Line label="Tax collected" value={formatMoney(expected.tax_collected)} />
              <Line label="Net sales" value={formatMoney(expected.net_sales)} strong />
              <Line label="Sealed invoice count" value={count(day.invoice_count)} />
              <Line label="Sealed void count" value={count(day.void_count)} />
            </dl>
          </section>

          <section>
            <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Accounts &amp; drawer
            </h3>
            <dl className="space-y-1 text-sm">
              <Line label="On-account sales" value={formatMoney(expected.on_account_sales)} />
              <Line label="Receipts collected" value={formatMoney(expected.receipts_collected)} />
              <Line label="Paid in" value={formatMoney(expected.paid_in)} />
              <Line label="Paid out" value={`-${formatMoney(expected.paid_out)}`} />
            </dl>
            <p className="mt-2 text-xs text-muted-foreground">
              On-account sales never enter a tender expectation — no money arrived in the till for them.
            </p>
          </section>
        </div>

        {z.movements.length > 0 && (
          <section>
            <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground">Cash movements</h3>
            <ul className="space-y-1 text-sm">
              {z.movements.map((m) => (
                <li key={m.id} className="flex flex-wrap items-baseline justify-between gap-2 border-b border-border py-1">
                  <span>
                    <span className="text-muted-foreground tabular-nums">{formatTimePK(m.created_at)}</span> {m.reason}
                    {m.created_by_name ? <span className="text-muted-foreground"> · {m.created_by_name}</span> : null}
                  </span>
                  <span className={cn('tabular-nums', m.movement_type === 'paid_out' ? 'text-destructive' : 'text-success-ink')}>
                    {m.movement_type === 'paid_out' ? '-' : '+'}
                    {formatMoney(m.amount)}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        )}

        {day.closing_notes && (
          <section>
            <h3 className="mb-1 text-sm font-semibold uppercase tracking-wide text-muted-foreground">Closing notes</h3>
            <p className="whitespace-pre-wrap text-sm">{day.closing_notes}</p>
          </section>
        )}
      </CardContent>
    </Card>
  )
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="text-sm font-medium">{value}</p>
    </div>
  )
}

function Line({ label, value, strong = false }: { label: string; value: string; strong?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn('tabular-nums', strong && 'font-semibold')}>{value}</dd>
    </div>
  )
}
