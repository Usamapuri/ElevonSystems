/**
 * The FBR queue (spec 2026-10-12 §2.6): every filing the till or the batch
 * screen could not finish inline, with what went wrong and a Retry for the
 * ones that died.
 */
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import apiClient from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { toast } from '@/hooks/use-toast'
import { formatDateTimePK } from '@/lib/print/format'
import type { FiscalJob } from '@/types'

type Filter = 'pending' | 'dead' | 'all'

function statusBadge(status: string): { label: string; variant: 'secondary' | 'success' | 'warning' | 'destructive' } {
  switch (status) {
    case 'succeeded':
      return { label: 'Filed', variant: 'success' }
    case 'dead':
      return { label: 'Failed', variant: 'destructive' }
    case 'processing':
      return { label: 'Filing…', variant: 'warning' }
    default:
      return { label: 'Waiting', variant: 'secondary' }
  }
}

export function FiscalQueueTab() {
  const qc = useQueryClient()
  const [filter, setFilter] = useState<Filter>('all')
  const jobs = useQuery({
    queryKey: ['fiscal', 'jobs', filter],
    queryFn: async (): Promise<FiscalJob[]> => {
      const res = await apiClient.listFiscalJobs(filter)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the queue')
      return res.data
    },
  })
  const retry = useMutation({
    mutationFn: (id: string) => apiClient.retryFiscalJob(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['fiscal'] })
      toast({ title: 'Queued again', variant: 'success' })
    },
    onError: (err: unknown) => toast({ title: 'Could not retry', description: err instanceof Error ? err.message : 'Unknown error', variant: 'destructive' }),
  })

  return (
    <div className="space-y-4">
      <div className="flex items-end gap-3">
        <div className="space-y-1.5">
          <Label htmlFor="fbr-queue-filter" className="text-xs text-muted-foreground">
            Show
          </Label>
          <Select value={filter} onValueChange={(v) => setFilter(v as Filter)}>
            <SelectTrigger id="fbr-queue-filter" className="w-[10rem]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All</SelectItem>
              <SelectItem value="pending">Waiting</SelectItem>
              <SelectItem value="dead">Failed</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      {jobs.error && <p className="text-sm text-destructive">{jobs.error instanceof Error ? jobs.error.message : 'Could not load'}</p>}
      <div className="overflow-x-auto rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Document</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="text-right">Attempts</TableHead>
              <TableHead>Last error</TableHead>
              <TableHead>Next run</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(jobs.data ?? []).length === 0 && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground">
                  {jobs.isLoading ? 'Loading…' : 'Nothing in the queue.'}
                </TableCell>
              </TableRow>
            )}
            {(jobs.data ?? []).map((j) => {
              const b = statusBadge(j.status)
              return (
                <TableRow key={j.id}>
                  <TableCell className="tabular font-medium">{j.document}</TableCell>
                  <TableCell className="text-muted-foreground">{j.kind === 'debit_note' ? 'Debit note' : j.kind === 'batch' ? 'Combined' : 'Sale'}</TableCell>
                  <TableCell>
                    <Badge variant={b.variant}>{b.label}</Badge>
                  </TableCell>
                  <TableCell className="tabular text-right">
                    {j.attempt_count}/{j.max_attempts}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {j.last_error_code ?? ''}
                    {j.last_error ? ` — ${j.last_error}` : ''}
                  </TableCell>
                  <TableCell className="tabular text-xs text-muted-foreground">{j.status === 'pending' ? formatDateTimePK(j.next_run_at) : ''}</TableCell>
                  <TableCell className="text-right">
                    {j.status === 'dead' && (
                      <Button size="sm" variant="outline" disabled={retry.isPending} onClick={() => retry.mutate(j.id)}>
                        Retry
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
