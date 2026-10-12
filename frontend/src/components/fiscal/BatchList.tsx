/**
 * Combined invoices already filed (or attempted): the last 30 days, each
 * row expanding to the sales it covered, with a tag on any voided since.
 */
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import apiClient from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatKgGrouped, formatMoney } from '@/lib/money'
import { formatBusinessDate } from '@/lib/print/format'
import type { EligibleInvoice, FiscalBatch } from '@/types'

function batchBadge(status: FiscalBatch['status']): { label: string; variant: 'success' | 'warning' | 'destructive' } {
  switch (status) {
    case 'synced':
      return { label: 'Filed', variant: 'success' }
    case 'failed':
      return { label: 'Rejected', variant: 'destructive' }
    default:
      return { label: 'Filing…', variant: 'warning' }
  }
}

function BatchRow({ batch }: { batch: FiscalBatch }) {
  const [open, setOpen] = useState(false)
  const detail = useQuery({
    queryKey: ['fiscal', 'batch', batch.id],
    enabled: open,
    queryFn: async (): Promise<EligibleInvoice[]> => {
      const res = await apiClient.getFiscalBatch(batch.id)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the batch')
      return res.data.invoices
    },
  })
  const badge = batchBadge(batch.status)
  return (
    <>
      <TableRow className="cursor-pointer" onClick={() => setOpen((o) => !o)}>
        <TableCell className="w-8 text-muted-foreground">{open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}</TableCell>
        <TableCell className="whitespace-nowrap">{formatBusinessDate(batch.business_date.slice(0, 10))}</TableCell>
        <TableCell>
          <Badge variant={badge.variant}>{badge.label}</Badge>
          {batch.last_error_code && <span className="ml-2 text-xs text-muted-foreground">{batch.last_error_code}</span>}
        </TableCell>
        <TableCell className="tabular">{batch.fbr_invoice_number ?? <span className="text-muted-foreground">—</span>}</TableCell>
        <TableCell className="tabular text-right">{batch.invoice_count}</TableCell>
        <TableCell className="tabular text-right">{formatKgGrouped(batch.total_kg)}</TableCell>
        <TableCell className="tabular text-right">{formatMoney(batch.total_amount)}</TableCell>
      </TableRow>
      {open && (
        <TableRow>
          <TableCell colSpan={7} className="bg-secondary/40 p-0">
            <ul className="divide-y divide-border px-10 py-2 text-sm">
              {detail.isLoading && <li className="py-1 text-muted-foreground">Loading…</li>}
              {(detail.data ?? []).map((r) => (
                <li key={r.id} className="flex items-center justify-between gap-3 py-1">
                  <span className="tabular">
                    {r.invoice_number}
                    {r.status === 'voided' && <Badge variant="destructive" className="ml-2">Voided later</Badge>}
                  </span>
                  <span className="tabular text-muted-foreground">
                    {formatKgGrouped(r.total_kg)} · {formatMoney(r.total_payable)}
                  </span>
                </li>
              ))}
            </ul>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

export function BatchList() {
  const to = new Date().toISOString().slice(0, 10)
  const from = new Date(Date.now() - 30 * 86_400_000).toISOString().slice(0, 10)
  const list = useQuery({
    queryKey: ['fiscal', 'batches', from, to],
    queryFn: async (): Promise<FiscalBatch[]> => {
      const res = await apiClient.listFiscalBatches(from, to)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the filings')
      return res.data
    },
  })
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-8" />
            <TableHead>Date</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>FBR number</TableHead>
            <TableHead className="text-right">Sales</TableHead>
            <TableHead className="text-right">Kg</TableHead>
            <TableHead className="text-right">Total</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(list.data ?? []).length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className="text-center text-muted-foreground">
                {list.isLoading ? 'Loading…' : 'No combined invoices in the last 30 days.'}
              </TableCell>
            </TableRow>
          )}
          {(list.data ?? []).map((b) => (
            <BatchRow key={b.id} batch={b} />
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
