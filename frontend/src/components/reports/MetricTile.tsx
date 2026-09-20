/**
 * A single headline figure above a report table — the same tile shape
 * `dashboard/KpiRow.tsx` already uses for "today", pulled out here so the
 * Reports tabs can show the selected window's own headline figures without
 * duplicating the markup per tab.
 *
 * RETAIL's `MetricTile` compares the figure against a previous period
 * (delta, percentage, a sparkline) via `MetricLabel`/`financeGlossary`,
 * neither of which exists in this codebase, and this backend does not
 * compute a "previous window" for a report the way the dashboard's 7d/30d
 * series does. This is deliberately the simpler, comparison-free shape —
 * a label, the figure, and an optional one-line hint — matching what
 * `reports.PeriodSummary` actually gives a report tab for its window.
 */
interface Props {
  label: string
  value: string
  hint?: string
}

export function MetricTile({ label, value, hint }: Props) {
  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-xl font-semibold tabular-nums">{value}</p>
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}
