/**
 * One panel per report tab (spec §6.8: Daily · Products · Tax · Cashiers ·
 * Hourly · Receivables · Day closes). Each panel is a thin shell around
 * `ReportTable`: it runs `GET /admin/reports/:name` for the shared date
 * range, shows the window's headline figures as `MetricTile`s where the
 * report has totals, and renders the table with that report's own column
 * list from `reportColumns.tsx`. Receivables and Day closes have no totals
 * (a position and a set of sealed rows, not a period to sum) and skip the
 * tile row entirely.
 */
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient from '@/api/client'
import { ExportButton } from './ExportButton'
import { HourlyHeatmap } from './HourlyHeatmap'
import { MetricTile } from './MetricTile'
import { ReportTable } from './ReportTable'
import { DayCloseDialog } from './DayCloseDialog'
import {
  cashierColumns, cashierTotals, dailyColumns, dailyTotals, dayCloseColumns,
  productColumns, productTotals, receivableColumns, taxColumns, taxTotals,
} from './reportColumns'
import { formatKgGrouped, formatMoney } from '@/lib/money'
import { formatBusinessDate } from '@/lib/print/format'
import type {
  CashierRow, DailyRow, DayCloseRow, HourRow, ProductRow, ReceivableRow, ReportName, ReportResponse, TaxBand,
} from '@/types'

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

/** One `GET /admin/reports/:name` call, typed per tab. */
function useReport<T>(name: ReportName, params: { from?: string; to?: string }, enabled: boolean) {
  return useQuery({
    queryKey: ['reports', name, params],
    queryFn: async (): Promise<ReportResponse<T>> => {
      const res = await apiClient.getReport<T>(name, params)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the report')
      return res.data
    },
    enabled,
  })
}

export interface PanelProps {
  from: string
  to: string
  params: { from?: string; to?: string }
  enabled: boolean
}

function PanelStatus({ isLoading, error }: { isLoading: boolean; error: unknown }) {
  return (
    <>
      {isLoading && (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading…
        </p>
      )}
      {error != null && <p className="text-sm text-destructive">{errorMessage(error, 'Could not load the report')}</p>}
    </>
  )
}

function PanelHeader({
  report, reportLabel, from, to, withPeriodPack, enabled,
}: {
  report: ReportName
  reportLabel: string
  from: string
  to: string
  withPeriodPack?: boolean
  enabled: boolean
}) {
  return (
    <div className="flex justify-end">
      <ExportButton report={report} reportLabel={reportLabel} from={from} to={to} withPeriodPack={withPeriodPack} disabled={!enabled} />
    </div>
  )
}

export function DailyPanel({ from, to, params, enabled }: PanelProps) {
  const q = useReport<DailyRow>('daily', params, enabled)
  const rows = q.data?.rows ?? []
  const totals = q.data?.totals

  return (
    <div className="space-y-4">
      <PanelHeader report="daily" reportLabel="Daily" from={from} to={to} withPeriodPack enabled={enabled} />
      {totals && (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <MetricTile label="Net sales" value={formatMoney(totals.net)} hint={`${totals.invoices} invoices, ${totals.voids} voided`} />
          <MetricTile label="Gross" value={formatMoney(totals.gross)} />
          <MetricTile label="Tax + further tax" value={formatMoney(totals.tax + totals.further_tax)} />
          <MetricTile label="Kg sold" value={formatKgGrouped(totals.kg_sold)} />
          <MetricTile label="Receipts collected" value={formatMoney(totals.receipts_total)} />
        </div>
      )}
      <PanelStatus isLoading={q.isLoading} error={q.error} />
      <ReportTable
        columns={dailyColumns}
        rows={rows}
        rowKey={(r) => r.business_date}
        totals={totals ? dailyTotals(totals) : null}
        emptyMessage="No sales in this range."
      />
    </div>
  )
}

export function ProductsPanel({ from, to, params, enabled }: PanelProps) {
  const q = useReport<ProductRow>('products', params, enabled)
  const rows = q.data?.rows ?? []
  const totals = q.data?.totals

  return (
    <div className="space-y-4">
      <PanelHeader report="products" reportLabel="Products" from={from} to={to} enabled={enabled} />
      {totals && (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <MetricTile label="Gross" value={formatMoney(totals.gross)} />
          <MetricTile label="Kg sold" value={formatKgGrouped(totals.kg_sold)} />
          <MetricTile label="Invoices" value={String(totals.invoices)} />
        </div>
      )}
      <PanelStatus isLoading={q.isLoading} error={q.error} />
      <ReportTable
        columns={productColumns}
        rows={rows}
        rowKey={(r, i) => r.product_id ?? `${r.name}-${i}`}
        totals={totals ? productTotals(totals) : null}
        emptyMessage="No products sold in this range."
      />
    </div>
  )
}

export function TaxPanel({ from, to, params, enabled }: PanelProps) {
  const q = useReport<TaxBand>('tax', params, enabled)
  const rows = q.data?.rows ?? []
  const totals = q.data?.totals

  return (
    <div className="space-y-4">
      <PanelHeader report="tax" reportLabel="Tax" from={from} to={to} enabled={enabled} />
      {totals && (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <MetricTile label="Taxable" value={formatMoney(totals.taxable)} />
          <MetricTile label="Tax" value={formatMoney(totals.tax)} />
          <MetricTile label="Further tax" value={formatMoney(totals.further_tax)} />
        </div>
      )}
      <PanelStatus isLoading={q.isLoading} error={q.error} />
      <ReportTable
        columns={taxColumns}
        rows={rows}
        rowKey={(r, i) => `${r.rate}-${i}`}
        totals={totals ? taxTotals(totals) : null}
        emptyMessage="No completed invoices in this range."
      />
    </div>
  )
}

export function CashiersPanel({ from, to, params, enabled }: PanelProps) {
  const q = useReport<CashierRow>('cashiers', params, enabled)
  const rows = q.data?.rows ?? []
  const totals = q.data?.totals

  return (
    <div className="space-y-4">
      <PanelHeader report="cashiers" reportLabel="Cashiers" from={from} to={to} enabled={enabled} />
      {totals && (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <MetricTile label="Gross" value={formatMoney(totals.gross)} />
          <MetricTile label="Invoices" value={String(totals.invoices)} hint={`${totals.voids} voided`} />
          <MetricTile label="Average" value={formatMoney(totals.invoices > 0 ? totals.gross / totals.invoices : 0)} />
        </div>
      )}
      <PanelStatus isLoading={q.isLoading} error={q.error} />
      <ReportTable
        columns={cashierColumns}
        rows={rows}
        rowKey={(r, i) => r.cashier_id ?? `${r.name}-${i}`}
        totals={totals ? cashierTotals(totals) : null}
        emptyMessage="No sales in this range."
      />
    </div>
  )
}

export function HourlyPanel({ from, to, params, enabled }: PanelProps) {
  const q = useReport<HourRow>('hourly', params, enabled)
  const rows = q.data?.rows ?? []
  const cells = q.data?.cells ?? []
  const totals = q.data?.totals

  return (
    <div className="space-y-4">
      <PanelHeader report="hourly" reportLabel="Hourly" from={from} to={to} enabled={enabled} />
      {totals && (
        <div className="grid gap-3 sm:grid-cols-2">
          <MetricTile label="Net sales" value={formatMoney(totals.net)} />
          <MetricTile label="Invoices" value={String(totals.invoices)} />
        </div>
      )}
      <PanelStatus isLoading={q.isLoading} error={q.error} />
      <HourlyHeatmap cells={cells} rows={rows} totals={totals ?? null} />
    </div>
  )
}

export function ReceivablesPanel({ from, to, params, enabled }: PanelProps) {
  // Receivables is a position as of `to`; the server ignores `from` for it
  // (backend/internal/handlers/reports.go receivablesView).
  const q = useReport<ReceivableRow>('receivables', params, enabled)
  const rows = q.data?.rows ?? []

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Every customer with an outstanding balance as of {formatBusinessDate(to)}, aged 0–30 / 31–60 / 61–90 / 90+ days
        FIFO against receipts. The From date does not change this — a balance is a position, not a period.
      </p>
      <PanelHeader report="receivables" reportLabel="Receivables" from={from} to={to} enabled={enabled} />
      <PanelStatus isLoading={q.isLoading} error={q.error} />
      <ReportTable
        columns={receivableColumns}
        rows={rows}
        rowKey={(r) => r.customer_id}
        totals={null}
        emptyMessage="Nobody owes the store anything as of this date."
      />
    </div>
  )
}

export function DayClosesPanel({ from, to, params, enabled }: PanelProps) {
  const q = useReport<DayCloseRow>('day-closes', params, enabled)
  const rows = q.data?.rows ?? []
  const [openDay, setOpenDay] = useState<{ id: string; label: string } | null>(null)

  const columns = dayCloseColumns((row) => setOpenDay({ id: row.id, label: `${formatBusinessDate(row.business_date)} — Z-report` }))

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        These are the figures sealed when the day was closed. A void entered after the close changes the Daily tab but
        never a sealed row.
      </p>
      <PanelHeader report="day-closes" reportLabel="Day closes" from={from} to={to} enabled={enabled} />
      <PanelStatus isLoading={q.isLoading} error={q.error} />
      <ReportTable
        columns={columns}
        rows={rows}
        rowKey={(r) => r.id}
        totals={null}
        emptyMessage="No business days in this range."
      />
      <DayCloseDialog
        dayId={openDay?.id ?? null}
        label={openDay?.label ?? ''}
        onOpenChange={(open) => !open && setOpenDay(null)}
      />
    </div>
  )
}
