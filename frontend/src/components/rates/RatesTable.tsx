import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow, tableInCard } from '@/components/ui/table'
import { ProductDialog } from '@/components/rates/ProductDialog'
import { toast } from '@/hooks/use-toast'
import { cn } from '@/lib/utils'
import type { Product, RateChange } from '@/types'

/** Rounds to paisa so string-vs-number and server-vs-client comparisons agree. */
function toPaisa(n: number): number {
  return Math.round(n * 100)
}

export function RatesTable() {
  const qc = useQueryClient()
  const [edits, setEdits] = useState<Record<string, string>>({})
  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<Product | null>(null)

  const { data: products, isLoading, error } = useQuery({
    queryKey: ['products'],
    queryFn: async () => {
      const res = await apiClient.getProducts({ active: true })
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load products')
      return res.data
    },
  })
  const rows = products ?? []

  const { changes, invalidIds } = useMemo(() => {
    const changes: RateChange[] = []
    const invalidIds = new Set<string>()
    for (const p of rows) {
      const raw = edits[p.id]
      if (raw === undefined || raw === '') continue
      const parsed = Number(raw)
      if (!Number.isFinite(parsed) || parsed < 0) {
        invalidIds.add(p.id)
        continue
      }
      if (toPaisa(parsed) === toPaisa(p.rate)) continue
      changes.push({ product_id: p.id, rate: Math.round(parsed * 100) / 100 })
    }
    return { changes, invalidIds }
  }, [rows, edits])

  const save = useMutation({
    mutationFn: () => apiClient.updateRates({ changes }),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['products'] })
      qc.invalidateQueries({ queryKey: ['rate-history'] })
      setEdits({})
      const n = res.data?.updated ?? 0
      toast({ title: `${n} rate${n === 1 ? '' : 's'} updated`, variant: 'success' })
    },
    onError: (err: unknown) => {
      toast({
        title: 'Could not save rates',
        description: err instanceof ApiClientError ? err.message : 'Unknown error',
        variant: 'destructive',
      })
    },
  })

  function valueFor(p: Product): string {
    return edits[p.id] ?? p.rate.toFixed(2)
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>Today&apos;s rates</CardTitle>
          <CardDescription>Edit the rate for one or more products, then save once. Every change is logged below.</CardDescription>
        </div>
        <Button onClick={() => setAddOpen(true)}>
          <Plus className="mr-2 h-4 w-4" /> Add product
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        {error && <p className="text-sm text-destructive">{error instanceof Error ? error.message : 'Could not load products'}</p>}
        <Table className={tableInCard}>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="hidden md:table-cell">SKU</TableHead>
              <TableHead className="hidden md:table-cell">HS code</TableHead>
              <TableHead className="text-right">Today&apos;s rate (Rs/kg)</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">Loading…</TableCell>
              </TableRow>
            )}
            {!isLoading && rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">No active products yet. Add one to get started.</TableCell>
              </TableRow>
            )}
            {rows.map((p) => {
              const isInvalid = invalidIds.has(p.id)
              const isChanged = changes.some((c) => c.product_id === p.id)
              return (
                <TableRow key={p.id} className={isChanged ? 'bg-warning-soft' : ''}>
                  <TableCell className="font-medium">{p.name}</TableCell>
                  <TableCell className="hidden md:table-cell text-muted-foreground">{p.sku ?? '—'}</TableCell>
                  <TableCell className="hidden md:table-cell text-muted-foreground">{p.hs_code ?? '—'}</TableCell>
                  <TableCell className="text-right">
                    <Input
                      type="number"
                      step="0.01"
                      min="0"
                      inputMode="decimal"
                      className={cn('ml-auto w-28 text-right', isInvalid && 'border-destructive focus-visible:ring-destructive')}
                      value={valueFor(p)}
                      onChange={(e) => setEdits((prev) => ({ ...prev, [p.id]: e.target.value }))}
                      aria-invalid={isInvalid}
                      aria-label={`Rate for ${p.name}`}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="ghost" size="sm" onClick={() => setEditing(p)} title="Edit" aria-label={`Edit ${p.name}`}>
                      <Pencil className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        <div className="flex items-center justify-end gap-3">
          {invalidIds.size > 0 && <p className="text-sm text-destructive">Fix the highlighted rate{invalidIds.size === 1 ? '' : 's'} before saving.</p>}
          <Button onClick={() => save.mutate()} disabled={changes.length === 0 || invalidIds.size > 0 || save.isPending}>
            {save.isPending ? 'Saving…' : changes.length > 0 ? `Save rates (${changes.length})` : 'Save rates'}
          </Button>
        </div>
      </CardContent>

      <ProductDialog open={addOpen} onOpenChange={setAddOpen} />
      <ProductDialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} product={editing ?? undefined} />
    </Card>
  )
}
