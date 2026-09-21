/**
 * The dashboard's KPI strip (spec §3): today's headline figures, the tender
 * mix (cash/card/online/on-account) and what customers still owe. Every
 * figure here comes straight off one `reports.PeriodSummary` (`today`) so it
 * can never disagree with the daily report row for the same date
 * (backend/internal/reports/summary.go).
 *
 * Whether the day is open is NOT shown here any more — `shell/PageHeader`
 * carries that pill on every screen, so repeating it in a tile of its own
 * would be the same fact twice on one page.
 *
 * `averageInvoice` and `tenderMix` are pure and live in ./kpis so they stay
 * unit-testable without mounting this component.
 */
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatKg, formatMoney } from '@/lib/money'
import { averageInvoice, tenderMix, type TenderMixEntry } from './kpis'
import type { PeriodSummary } from '@/types'

interface Props {
  today: PeriodSummary
  receivablesOutstanding: number
}

const TENDER_LABELS: { key: keyof ReturnType<typeof tenderMix>; label: string }[] = [
  { key: 'cash', label: 'Cash' },
  { key: 'card', label: 'Card' },
  { key: 'online', label: 'Online' },
  { key: 'on_account', label: 'On account' },
]

export function KpiRow({ today, receivablesOutstanding }: Props) {
  const avg = averageInvoice(today.net, today.invoices)
  const mix = tenderMix(today.tenders)

  return (
    <div className="space-y-4">
      {/*
        The same navy plate the till ends on, used once here for the two
        figures the owner actually opens this screen for. Everything below is
        deliberately quiet — if five tiles all shout, none of them does.
      */}
      <div className="grid gap-px overflow-hidden rounded-lg bg-white/10 sm:grid-cols-2">
        <PlateFigure label="Net revenue today" value={formatMoney(today.net)} hint="Voids excluded" />
        <PlateFigure label="Kg sold today" value={formatKg(today.kg_sold)} hint={`Across ${today.invoices} ${today.invoices === 1 ? 'sale' : 'sales'}`} />
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        <Tile label="Invoices" value={String(today.invoices)} hint={`${today.voids} voided`} />
        <Tile label="Average invoice" value={formatMoney(avg)} hint="Net divided by invoices" />
        <Tile label="Receivables outstanding" value={formatMoney(receivablesOutstanding)} hint="What customers owe, as of today" />
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Tender mix today</CardTitle>
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

function PlateFigure({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="min-w-0 bg-rail px-5 py-4">
      <p className="text-xs font-bold text-rail-muted">{label}</p>
      <p className="tabular mt-1 truncate text-[2rem] font-extrabold leading-tight text-primary">{value}</p>
      {hint && <p className="text-xs font-medium text-rail-muted">{hint}</p>}
    </div>
  )
}

function Tile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <p className="text-xs font-semibold text-muted-foreground">{label}</p>
      <p className="tabular mt-1 text-xl font-bold">{value}</p>
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

function TenderStat({ label, entry }: { label: string; entry: TenderMixEntry }) {
  return (
    <div>
      <p className="text-xs font-semibold text-muted-foreground">{label}</p>
      <p className="tabular text-base font-bold">{formatMoney(entry.amount)}</p>
      <p className="tabular text-xs text-muted-foreground">{entry.pct}% of today</p>
    </div>
  )
}
