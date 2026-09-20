/**
 * Column definitions and totals-row builders for every report tab (spec
 * §6.8). Labels mirror the backend's own CSV/xlsx headers
 * (backend/internal/handlers/reports.go `dailyView` et al.) so what the
 * screen shows and what a downloaded file says line up exactly.
 *
 * Two formatting rules to keep straight, both pinned by the backend's own
 * comments (backend/internal/reports/queries.go):
 *   - `TaxBand.rate` is a **fraction** (0.18) — `percentLabel` multiplies by
 *     100 before rendering it.
 *   - `ProductRow.share` is **already a percentage** (42.5, not 0.425) —
 *     `plainPercentLabel` renders it as-is.
 */
import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import type { ReportColumn } from './ReportTable'
import { formatKg, formatMoney } from '@/lib/money'
import { formatBusinessDate, percentLabel, plainPercentLabel } from '@/lib/print/format'
import type {
  CashierRow, DailyRow, DayCloseRow, HourRow, PeriodSummary, ProductRow, ReceivableRow, TaxBand,
} from '@/types'

const DASH = '—'

function hourLabel(hour: number): string {
  return `${String(hour).padStart(2, '0')}:00`
}

// ── Daily ────────────────────────────────────────────────────────────────

export const dailyColumns: ReportColumn<DailyRow>[] = [
  { key: 'date', label: 'Date', format: (r) => formatBusinessDate(r.business_date) },
  { key: 'invoices', label: 'Invoices', align: 'right', format: (r) => String(r.invoices) },
  { key: 'voids', label: 'Voids', align: 'right', format: (r) => String(r.voids) },
  { key: 'kg', label: 'Kg', align: 'right', format: (r) => formatKg(r.kg_sold) },
  { key: 'gross', label: 'Gross', align: 'right', format: (r) => formatMoney(r.gross) },
  { key: 'discount', label: 'Discount', align: 'right', format: (r) => formatMoney(r.discount) },
  { key: 'taxable', label: 'Taxable', align: 'right', format: (r) => formatMoney(r.taxable) },
  { key: 'tax', label: 'Tax', align: 'right', format: (r) => formatMoney(r.tax) },
  { key: 'further_tax', label: 'Further Tax', align: 'right', format: (r) => formatMoney(r.further_tax) },
  { key: 'rounding', label: 'Rounding', align: 'right', format: (r) => formatMoney(r.rounding) },
  { key: 'net', label: 'Net', align: 'right', format: (r) => formatMoney(r.net) },
  { key: 'cash', label: 'Cash', align: 'right', format: (r) => formatMoney(r.tenders.cash) },
  { key: 'card', label: 'Card', align: 'right', format: (r) => formatMoney(r.tenders.card) },
  { key: 'online', label: 'Online', align: 'right', format: (r) => formatMoney(r.tenders.online) },
  { key: 'on_account', label: 'On Account', align: 'right', format: (r) => formatMoney(r.tenders.on_account) },
  { key: 'receipts', label: 'Receipts', align: 'right', format: (r) => formatMoney(r.receipts_total) },
]

export function dailyTotals(t: PeriodSummary): Record<string, ReactNode> {
  return {
    date: 'Total',
    invoices: String(t.invoices),
    voids: String(t.voids),
    kg: formatKg(t.kg_sold),
    gross: formatMoney(t.gross),
    discount: formatMoney(t.discount),
    taxable: formatMoney(t.taxable),
    tax: formatMoney(t.tax),
    further_tax: formatMoney(t.further_tax),
    rounding: formatMoney(t.rounding),
    net: formatMoney(t.net),
    cash: formatMoney(t.tenders.cash),
    card: formatMoney(t.tenders.card),
    online: formatMoney(t.tenders.online),
    on_account: formatMoney(t.tenders.on_account),
    receipts: formatMoney(t.receipts_total),
  }
}

// ── Products ─────────────────────────────────────────────────────────────

export const productColumns: ReportColumn<ProductRow>[] = [
  { key: 'name', label: 'Product', format: (r) => r.name },
  { key: 'kg', label: 'Kg', align: 'right', format: (r) => formatKg(r.kg) },
  { key: 'invoices', label: 'Invoices', align: 'right', format: (r) => String(r.invoices) },
  { key: 'gross', label: 'Gross', align: 'right', format: (r) => formatMoney(r.gross) },
  { key: 'share', label: 'Share %', align: 'right', format: (r) => plainPercentLabel(r.share) },
]

export function productTotals(t: PeriodSummary): Record<string, ReactNode> {
  return {
    name: 'Total',
    kg: formatKg(t.kg_sold),
    invoices: String(t.invoices),
    gross: formatMoney(t.gross),
    share: t.gross !== 0 ? plainPercentLabel(100) : DASH,
  }
}

// ── Tax ──────────────────────────────────────────────────────────────────

export const taxColumns: ReportColumn<TaxBand>[] = [
  { key: 'rate', label: 'Rate %', align: 'right', format: (r) => percentLabel(r.rate) },
  { key: 'invoices', label: 'Invoices', align: 'right', format: (r) => String(r.invoices) },
  { key: 'taxable', label: 'Taxable', align: 'right', format: (r) => formatMoney(r.taxable) },
  { key: 'tax', label: 'Tax', align: 'right', format: (r) => formatMoney(r.tax) },
  { key: 'further_tax', label: 'Further Tax', align: 'right', format: (r) => formatMoney(r.further_tax) },
]

export function taxTotals(t: PeriodSummary): Record<string, ReactNode> {
  return {
    rate: 'Total',
    invoices: String(t.invoices),
    taxable: formatMoney(t.taxable),
    tax: formatMoney(t.tax),
    further_tax: formatMoney(t.further_tax),
  }
}

// ── Cashiers ─────────────────────────────────────────────────────────────

export const cashierColumns: ReportColumn<CashierRow>[] = [
  { key: 'name', label: 'Cashier', format: (r) => r.name || DASH },
  { key: 'invoices', label: 'Invoices', align: 'right', format: (r) => String(r.invoices) },
  { key: 'gross', label: 'Gross', align: 'right', format: (r) => formatMoney(r.gross) },
  { key: 'average', label: 'Average', align: 'right', format: (r) => formatMoney(r.average) },
  { key: 'voids', label: 'Voids', align: 'right', format: (r) => String(r.voids) },
]

export function cashierTotals(t: PeriodSummary): Record<string, ReactNode> {
  const average = t.invoices > 0 ? t.gross / t.invoices : 0
  return {
    name: 'Total',
    invoices: String(t.invoices),
    gross: formatMoney(t.gross),
    average: formatMoney(average),
    voids: String(t.voids),
  }
}

// ── Hourly ───────────────────────────────────────────────────────────────

export const hourlyColumns: ReportColumn<HourRow>[] = [
  { key: 'hour', label: 'Hour', format: (r) => hourLabel(r.hour) },
  { key: 'invoices', label: 'Invoices', align: 'right', format: (r) => String(r.invoices) },
  { key: 'net', label: 'Net', align: 'right', format: (r) => formatMoney(r.net) },
]

export function hourlyTotals(t: PeriodSummary): Record<string, ReactNode> {
  return {
    hour: 'Total',
    invoices: String(t.invoices),
    net: formatMoney(t.net),
  }
}

// ── Receivables (a position — no totals row; spec §6.8) ────────────────────

/** The customer name links to the customer list (`/customers`) — the route
 * has no `?id=` deep-link to open one customer's detail directly, so the
 * link lands the owner on the list they can search from rather than a page
 * that does not exist. */
export const receivableColumns: ReportColumn<ReceivableRow>[] = [
  {
    key: 'name',
    label: 'Customer',
    format: (r) => (
      <Link to="/customers" className="font-medium text-primary hover:underline">
        {r.name}
      </Link>
    ),
  },
  { key: 'phone', label: 'Phone', format: (r) => r.phone || DASH },
  { key: 'balance', label: 'Balance', align: 'right', format: (r) => formatMoney(r.balance) },
  { key: 'b0_30', label: '0-30', align: 'right', format: (r) => formatMoney(r.b0_30) },
  { key: 'b31_60', label: '31-60', align: 'right', format: (r) => formatMoney(r.b31_60) },
  { key: 'b61_90', label: '61-90', align: 'right', format: (r) => formatMoney(r.b61_90) },
  { key: 'b90', label: '90+', align: 'right', format: (r) => formatMoney(r.b90) },
  { key: 'last_receipt', label: 'Last Receipt', format: (r) => (r.last_receipt ? formatBusinessDate(r.last_receipt) : DASH) },
]

// ── Day closes (sealed rows — no totals row; spec §6.8) ─────────────────────

function statusLabel(status: DayCloseRow['status']): string {
  switch (status) {
    case 'open':
      return 'Open'
    case 'reopened':
      return 'Reopened'
    case 'closed':
      return 'Closed'
    default:
      return status
  }
}

/** A signed money figure — mirrors `ZReportView`'s own local `signedMoney`,
 * so a variance carries the same `+`/`-` sign convention wherever it is
 * shown (the day-close screen's tender table and this report row for the
 * same day should read identically). */
function signedMoney(n: number): string {
  if (n === 0) return formatMoney(0)
  return (n > 0 ? '+' : '-') + formatMoney(Math.abs(n))
}

/** `onOpenZReport` is supplied by the tab, which owns the dialog's open
 * state — this module stays free of any component state of its own. */
export function dayCloseColumns(onOpenZReport: (row: DayCloseRow) => void): ReportColumn<DayCloseRow>[] {
  return [
    { key: 'date', label: 'Date', format: (r) => formatBusinessDate(r.business_date) },
    { key: 'status', label: 'Status', format: (r) => statusLabel(r.status) },
    { key: 'closed_by', label: 'Closed By', format: (r) => r.closed_by || DASH },
    { key: 'expected_cash', label: 'Expected Cash', align: 'right', format: (r) => formatMoney(r.expected_cash) },
    { key: 'counted_cash', label: 'Counted Cash', align: 'right', format: (r) => formatMoney(r.counted_cash) },
    { key: 'cash_variance', label: 'Cash Variance', align: 'right', format: (r) => signedMoney(r.cash_variance) },
    { key: 'card_variance', label: 'Card Variance', align: 'right', format: (r) => signedMoney(r.card_variance) },
    { key: 'online_variance', label: 'Online Variance', align: 'right', format: (r) => signedMoney(r.online_variance) },
    { key: 'net', label: 'Net', align: 'right', format: (r) => formatMoney(r.net) },
    {
      key: 'z_report',
      label: 'Z-report',
      align: 'right',
      format: (r) => (
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={(e) => {
            e.stopPropagation()
            onOpenZReport(r)
          }}
        >
          View
        </Button>
      ),
    },
  ]
}
