/**
 * File with FBR (spec 2026-10-12 §2.3): the business date's unfiled sales,
 * all ticked by default, filed together as one combined FBR invoice. Every
 * figure shown or sent is a sum of recorded sales; there is no way to type
 * a quantity or an amount here.
 */
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { toast } from '@/hooks/use-toast'
import { fiscalStatusLabel, ineligibleReason, selectionTotals } from '@/lib/fiscal'
import { formatKgGrouped, formatMoney } from '@/lib/money'
import { formatTimePK } from '@/lib/print/format'
import { cn } from '@/lib/utils'
import type { EligibleInvoice, FiscalBatch, FiscalPending } from '@/types'

export const FISCAL_PENDING_KEY = ['fiscal', 'pending'] as const
export const FISCAL_BATCHES_KEY = ['fiscal', 'batches'] as const

interface Props {
  onFiled: (batch: FiscalBatch) => void
}

export function FileWithFbr({ onFiled }: Props) {
  const qc = useQueryClient()
  // The business date comes from the server (GET /fiscal/pending), never
  // the browser's clock: at 01:00 PKT the UTC date is still yesterday.
  const [date, setDate] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirm, setConfirm] = useState(false)
  const pending = useQuery({
    queryKey: FISCAL_PENDING_KEY,
    queryFn: async (): Promise<FiscalPending> => {
      const res = await apiClient.getFiscalPending()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load')
      return res.data
    },
  })
  useEffect(() => {
    if (date === '' && pending.data?.business_date) setDate(pending.data.business_date)
  }, [date, pending.data])

  const eligible = useQuery({
    queryKey: ['fiscal', 'eligible', date],
    enabled: date !== '',
    queryFn: async (): Promise<EligibleInvoice[]> => {
      const res = await apiClient.getFiscalEligible(date)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the unfiled sales')
      return res.data
    },
  })
  const rows = useMemo(() => eligible.data ?? [], [eligible.data])

  // Everything eligible is ticked when the list (re)loads; the operator
  // unticks what they want to leave out.
  useEffect(() => {
    setSelected(new Set(rows.filter((r) => r.eligible).map((r) => r.id)))
  }, [rows])

  const totals = selectionTotals(rows, selected)

  const file = useMutation({
    mutationFn: () => apiClient.createFiscalBatch(date, [...selected]),
    onSuccess: (res) => {
      setConfirm(false)
      qc.invalidateQueries({ queryKey: ['fiscal'] })
      qc.invalidateQueries({ queryKey: ['invoices'] })
      if (res.data) onFiled(res.data)
    },
    onError: (err: unknown) => {
      setConfirm(false)
      const code = err instanceof ApiClientError ? err.code : undefined
      if (code === 'fiscal_batch_ineligible') {
        qc.invalidateQueries({ queryKey: ['fiscal', 'eligible'] })
        toast({ title: 'Some invoices changed — the list has been refreshed', variant: 'destructive' })
        return
      }
      toast({ title: 'Could not file with FBR', description: err instanceof Error ? err.message : 'Unknown error', variant: 'destructive' })
    },
  })

  const toggle = (id: string, on: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  }
  const allEligible = rows.filter((r) => r.eligible)
  const allTicked = allEligible.length > 0 && allEligible.every((r) => selected.has(r.id))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1.5">
          <Label htmlFor="fbr-date">Business date</Label>
          <Input id="fbr-date" type="date" value={date} onChange={(e) => setDate(e.target.value)} className="w-[11rem]" />
        </div>
        {eligible.isFetching && <Loader2 className="mb-2 h-4 w-4 animate-spin text-muted-foreground" />}
      </div>

      {eligible.error && <p className="text-sm text-destructive">{eligible.error instanceof Error ? eligible.error.message : 'Could not load'}</p>}

      <div className="overflow-x-auto rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10">
                <Checkbox
                  aria-label="Select all eligible"
                  checked={allTicked}
                  disabled={allEligible.length === 0}
                  onCheckedChange={(on) => setSelected(on ? new Set(allEligible.map((r) => r.id)) : new Set())}
                />
              </TableHead>
              <TableHead>Invoice</TableHead>
              <TableHead>Time</TableHead>
              <TableHead>Customer</TableHead>
              <TableHead className="text-right">Kg</TableHead>
              <TableHead className="text-right">Total</TableHead>
              <TableHead>FBR</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 && !eligible.isLoading && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground">
                  Nothing unfiled on this date.
                </TableCell>
              </TableRow>
            )}
            {rows.map((r) => (
              <TableRow key={r.id} className={cn(!r.eligible && 'opacity-60')}>
                <TableCell>
                  <Checkbox
                    aria-label={`Select ${r.invoice_number}`}
                    checked={selected.has(r.id)}
                    disabled={!r.eligible}
                    onCheckedChange={(on) => toggle(r.id, on)}
                  />
                </TableCell>
                <TableCell className="tabular font-medium">
                  {r.invoice_number}
                  {!r.eligible && <span className="block text-xs text-muted-foreground">{ineligibleReason(r.reason)}</span>}
                </TableCell>
                <TableCell className="tabular text-muted-foreground">{r.created_at ? formatTimePK(r.created_at) : ''}</TableCell>
                <TableCell>{r.customer_name ?? <span className="text-muted-foreground">Walk-in</span>}</TableCell>
                <TableCell className="tabular text-right">{formatKgGrouped(r.total_kg)}</TableCell>
                <TableCell className="tabular text-right">{formatMoney(r.total_payable)}</TableCell>
                <TableCell className="text-xs text-muted-foreground">{fiscalStatusLabel(r.fiscal_status)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-3">
        <p className="tabular text-sm">
          <span className="font-semibold">{totals.count}</span> of {allEligible.length} selected · {formatKgGrouped(totals.kg)} · {formatMoney(totals.total_payable)}
        </p>
        <Button disabled={totals.count === 0 || file.isPending} onClick={() => setConfirm(true)}>
          File selected with FBR
        </Button>
      </div>

      <Dialog open={confirm} onOpenChange={(open) => !file.isPending && setConfirm(open)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>File one combined invoice with FBR?</DialogTitle>
            <DialogDescription>
              {totals.count} {totals.count === 1 ? 'sale' : 'sales'} dated {date} · {formatKgGrouped(totals.kg)} · {formatMoney(totals.total_payable)}. Each sale’s
              lines are sent exactly as recorded.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="gap-2 sm:gap-2">
            <Button variant="outline" disabled={file.isPending} onClick={() => setConfirm(false)}>
              Back
            </Button>
            <Button disabled={file.isPending} onClick={() => file.mutate()}>
              {file.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'File now'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
