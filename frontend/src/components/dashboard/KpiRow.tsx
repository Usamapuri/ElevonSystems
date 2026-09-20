/**
 * The dashboard's KPI strip (spec §3): today's headline figures, the tender
 * mix (cash/card/online/on-account), what customers still owe, and whether
 * the till is even open. Every figure here comes straight off one
 * `reports.PeriodSummary` (`today`) so it can never disagree with the daily
 * report row for the same date (backend/internal/reports/summary.go).
 *
 * `averageInvoice` and `tenderMix` are pure and live in ./kpis so they stay
 * unit-testable without mounting this component.
 */
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatKg, formatMoney } from '@/lib/money'
import { averageInvoice, tenderMix, type TenderMixEntry } from './kpis'
import type { DashboardDay, PeriodSummary } from '@/types'

interface Props {
  today: PeriodSummary
  receivablesOutstanding: number
  day: DashboardDay | null
}

const TENDER_LABELS: { key: keyof ReturnType<typeof tenderMix>; label: string }[] = [
  { key: 'cash', label: 'Cash' },
  { key: 'card', label: 'Card' },
  { key: 'online', label: 'Online' },
  { key: 'on_account', label: 'On account' },
]

function dayStatus(day: DashboardDay | null): { label: string; variant: 'success' | 'secondary' | 'warning' } {
  if (!day) return { label: 'Not opened', variant: 'secondary' }
  switch (day.status) {
    case 'open':
      return { label: 'Open', variant: 'success' }
    case 'reopened':
      return { label: 'Reopened', variant: 'warning' }
    case 'closed':
      return { label: 'Closed', variant: 'secondary' }
    default:
      return { label: day.status, variant: 'secondary' }
  }
}

export function KpiRow({ today, receivablesOutstanding, day }: Props) {
  const avg = averageInvoice(today.net, today.invoices)
  const mix = tenderMix(today.tenders)
  const status = dayStatus(day)

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
        <Tile label="Net revenue" value={formatMoney(today.net)} hint="Today, voids excluded" />
        <Tile label="Kg sold" value={formatKg(today.kg_sold)} hint="Today" />
        <Tile label="Invoices" value={String(today.invoices)} hint={`${today.voids} voided`} />
        <Tile label="Average invoice" value={formatMoney(avg)} hint="Net ÷ invoices" />
        <Tile label="Receivables outstanding" value={formatMoney(receivablesOutstanding)} hint="What customers owe, as of today" />
        <div className="rounded-xl border border-border bg-card p-4">
          <p className="text-xs uppercase tracking-wide text-muted-foreground">Business day</p>
          <div className="mt-2">
            <Badge variant={status.variant} className="uppercase">
              {status.label}
            </Badge>
          </div>
        </div>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-semibold">Tender mix — today</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {TENDER_LABELS.map(({ key, label }) => (
              <TenderStat key={key} label={label} entry={mix[key]} />
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function Tile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-xl font-semibold tabular-nums">{value}</p>
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

function TenderStat({ label, entry }: { label: string; entry: TenderMixEntry }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="text-sm font-medium tabular-nums">{formatMoney(entry.amount)}</p>
      <p className="text-xs text-muted-foreground tabular-nums">{entry.pct}%</p>
    </div>
  )
}
