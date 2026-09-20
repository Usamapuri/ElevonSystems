import { useQuery } from '@tanstack/react-query'
import apiClient from '@/api/client'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatMoney } from '@/lib/money'

const HISTORY_LIMIT = 50

function formatWhen(iso: string): string {
  return new Date(iso).toLocaleString('en-PK', { dateStyle: 'medium', timeStyle: 'short' })
}

/** Last 50 rate changes, newest first — who changed what and when. */
export function RateHistoryCard() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['rate-history'],
    queryFn: async () => {
      const res = await apiClient.getRateHistory({ limit: HISTORY_LIMIT })
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load rate history')
      return res.data
    },
  })
  const entries = data ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>Rate history</CardTitle>
        <CardDescription>Last {HISTORY_LIMIT} changes, newest first.</CardDescription>
      </CardHeader>
      <CardContent>
        {error && <p className="text-sm text-red-600">{error instanceof Error ? error.message : 'Could not load rate history'}</p>}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>When</TableHead>
              <TableHead>Product</TableHead>
              <TableHead className="hidden md:table-cell">Who</TableHead>
              <TableHead className="text-right">Old → New</TableHead>
              <TableHead className="hidden md:table-cell">Note</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">Loading…</TableCell>
              </TableRow>
            )}
            {!isLoading && entries.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">No rate changes yet.</TableCell>
              </TableRow>
            )}
            {entries.map((e) => (
              <TableRow key={e.id}>
                <TableCell className="text-muted-foreground">{formatWhen(e.changed_at)}</TableCell>
                <TableCell className="font-medium">{e.product_name}</TableCell>
                <TableCell className="hidden md:table-cell">{e.changed_by_name ?? '—'}</TableCell>
                <TableCell className="text-right">
                  {formatMoney(e.old_rate)} → {formatMoney(e.new_rate)}
                </TableCell>
                <TableCell className="hidden md:table-cell text-muted-foreground">{e.note ?? '—'}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
