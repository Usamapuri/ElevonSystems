/**
 * Ageing card: the receivables report's own row for this one customer
 * (spec §6.5, §6.8) — balance plus the 0–30 / 31–60 / 61–90 / 90+ day
 * buckets, and the date they last paid. `GET /customers/:id/ageing` runs
 * the same `reports.Receivables` the report does and picks this customer's
 * row out of it, so this card can never disagree with the report.
 */
import { useQuery } from '@tanstack/react-query'
import apiClient from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatMoney } from '@/lib/money'
import { formatBusinessDate } from '@/lib/print/format'
import type { CustomerAgeing } from '@/types'

interface Props {
  customerId: string
  /** Only fetches while the sheet holding this card is actually open. */
  enabled: boolean
}

const BUCKETS: { key: keyof Pick<CustomerAgeing, 'b0_30' | 'b31_60' | 'b61_90' | 'b90'>; label: string }[] = [
  { key: 'b0_30', label: '0–30 days' },
  { key: 'b31_60', label: '31–60 days' },
  { key: 'b61_90', label: '61–90 days' },
  { key: 'b90', label: '90+ days' },
]

export function AgeingCard({ customerId, enabled }: Props) {
  const { data, isLoading, error } = useQuery({
    queryKey: ['ageing', customerId],
    queryFn: async () => {
      const res = await apiClient.getCustomerAgeing(customerId)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the ageing')
      return res.data
    },
    enabled,
  })

  if (isLoading) return <p className="text-sm text-muted-foreground">Loading ageing…</p>
  if (error) {
    return <p className="text-sm text-red-600">{error instanceof Error ? error.message : 'Could not load the ageing'}</p>
  }
  if (!data) return null

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-semibold">Ageing</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {BUCKETS.map(({ key, label }) => (
            <div key={key} className="text-center">
              <p className="text-xs text-muted-foreground">{label}</p>
              <p className={`text-sm font-medium tabular-nums ${data[key] > 0 ? 'text-amber-600' : ''}`}>
                {formatMoney(data[key])}
              </p>
            </div>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          Last payment: {data.last_receipt ? formatBusinessDate(data.last_receipt) : '—'}
        </p>
      </CardContent>
    </Card>
  )
}
