/**
 * The admin home screen (spec §3, Task P4): today's KPIs, the 7d/30d
 * revenue-and-kg trend, the last 30 days' top products and the last few
 * sales, all from one `GET /admin/dashboard` round trip
 * (backend/internal/handlers/reports.go). Polls every 30 seconds so the
 * owner can leave it open on a second screen; a live day-status pill and
 * receivables figure are exactly the kind of thing that goes stale fast
 * enough that a manual refresh is not good enough.
 *
 * Route access is gated in `_app.tsx`'s `beforeLoad` via `lib/roles.ts`
 * (`dashboard` is admin-only there already) — nothing to add here.
 */
import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient from '@/api/client'
import { KpiRow } from '@/components/dashboard/KpiRow'
import { RecentInvoices } from '@/components/dashboard/RecentInvoices'
import { TopProducts } from '@/components/dashboard/TopProducts'
import { TrendChart } from '@/components/dashboard/TrendChart'
import { formatBusinessDate } from '@/lib/print/format'

export const Route = createFileRoute('/_app/dashboard')({ component: DashboardPage })

const DASHBOARD_KEY = ['dashboard'] as const
const POLL_MS = 30_000

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

function DashboardPage() {
  const { data, isLoading, isFetching, error } = useQuery({
    queryKey: DASHBOARD_KEY,
    queryFn: async () => {
      const res = await apiClient.getDashboard()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the dashboard')
      return res.data
    },
    refetchInterval: POLL_MS,
  })

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold">Dashboard</h1>
          <p className="text-sm text-muted-foreground">
            {data ? formatBusinessDate(data.today.from) : 'Today'}
          </p>
        </div>
        {isFetching && !isLoading && (
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> Refreshing…
          </span>
        )}
      </div>

      {error && <p className="text-sm text-destructive">{errorMessage(error, 'Could not load the dashboard')}</p>}
      {isLoading && (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading the dashboard…
        </p>
      )}

      {data && (
        <>
          <KpiRow today={data.today} receivablesOutstanding={data.receivables_outstanding} day={data.day} />
          <TrendChart series7d={data.series_7d} series30d={data.series_30d} />
          <div className="grid gap-4 xl:grid-cols-2">
            <TopProducts products={data.top_products} />
            <RecentInvoices invoices={data.recent_invoices} />
          </div>
        </>
      )}
    </div>
  )
}
