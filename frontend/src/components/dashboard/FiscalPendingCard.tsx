/**
 * "Unfiled with FBR" on the dashboard (spec 2026-10-12 §2.4). Information
 * only: it links to the filing screen and never warns.
 */
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import apiClient from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatKgGrouped, formatMoney } from '@/lib/money'
import type { FiscalPending } from '@/types'

export function FiscalPendingCard() {
  const q = useQuery({
    queryKey: ['fiscal', 'pending'],
    refetchInterval: 30_000,
    queryFn: async (): Promise<FiscalPending> => {
      const res = await apiClient.getFiscalPending()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load')
      return res.data
    },
  })
  if (!q.data?.enabled) return null
  const { today, all } = q.data
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Unfiled with FBR</CardTitle>
      </CardHeader>
      <CardContent className="space-y-1 text-sm">
        <p className="tabular">
          Today: <span className="font-semibold">{today.count}</span> {today.count === 1 ? 'invoice' : 'invoices'} · {formatKgGrouped(today.kg)} · {formatMoney(today.total_payable)}
        </p>
        <p className="tabular text-muted-foreground">
          In all: {all.count} · {formatKgGrouped(all.kg)} · {formatMoney(all.total_payable)}
        </p>
        <Link to="/fbr" className="inline-block pt-1 text-sm font-semibold text-primary underline-offset-4 hover:underline">
          File with FBR
        </Link>
      </CardContent>
    </Card>
  )
}
