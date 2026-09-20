/**
 * The last few sales, with the two things a counter needs after the fact:
 * print it again, and void it.
 *
 * A void is whole-invoice, needs a written reason and an admin PIN, and
 * leaves the invoice exactly where it is with status `voided` — still
 * listed, still printable, excluded from every total (spec §6.4). The PIN
 * identifies the admin who authorised it; the signed-in cashier is recorded
 * as who did it.
 */
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Ban, Printer, RefreshCw } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Money } from '@/components/shared/Money'
import { PinEntryModal } from '@/components/shared/PinEntryModal'
import { toast } from '@/hooks/use-toast'
import { printInvoice } from '@/lib/print/printInvoice'
import { cn } from '@/lib/utils'
import type { Invoice, PrintDocument } from '@/types'

export const RECENT_INVOICES_KEY = ['invoices', 'recent'] as const

/** HH:MM off the created_at timestamp, in the browser's zone (the till's). */
function timeOf(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString('en-PK', { hour: '2-digit', minute: '2-digit', hour12: false })
}

export function RecentInvoices() {
  const qc = useQueryClient()
  const [voiding, setVoiding] = useState<Invoice | null>(null)
  const [voidError, setVoidError] = useState<string | null>(null)

  const { data, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: RECENT_INVOICES_KEY,
    queryFn: async () => {
      const res = await apiClient.getRecentInvoices({ limit: 10 })
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load recent sales')
      return res.data
    },
  })
  const invoices = data ?? []

  /** A listing carries no lines, so a reprint re-reads the invoice first. */
  const reprint = async (invoice: Invoice, document: PrintDocument) => {
    try {
      const res = await apiClient.getInvoice(invoice.id)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the invoice')
      await printInvoice(res.data, document)
    } catch (err) {
      toast({
        title: 'Could not reprint',
        description: err instanceof Error ? err.message : 'Unknown error',
        variant: 'destructive',
      })
    }
  }

  const voidMutation = useMutation({
    mutationFn: ({ id, pin, reason }: { id: string; pin: string; reason: string }) =>
      apiClient.voidInvoice(id, { pin, reason }),
    onSuccess: (res) => {
      setVoiding(null)
      setVoidError(null)
      qc.invalidateQueries({ queryKey: RECENT_INVOICES_KEY })
      qc.invalidateQueries({ queryKey: ['day', 'current'] })
      qc.invalidateQueries({ queryKey: ['customers'] })
      toast({ title: `Invoice ${res.data?.invoice_number ?? ''} voided`, variant: 'success' })
    },
    onError: (err: unknown) => {
      if (err instanceof ApiClientError && err.code === 'invoice_already_voided') {
        setVoiding(null)
        setVoidError(null)
        qc.invalidateQueries({ queryKey: RECENT_INVOICES_KEY })
        toast({ title: 'That invoice was already voided', variant: 'destructive' })
        return
      }
      setVoidError(err instanceof Error ? err.message : 'Could not void the invoice')
    },
  })

  return (
    <div className="shrink-0 rounded-xl border border-border bg-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Recent sales</h2>
        <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="Refresh" onClick={() => refetch()}>
          <RefreshCw className={cn('h-4 w-4', isFetching && 'animate-spin')} />
        </Button>
      </div>

      {error && <p className="text-sm text-destructive">Could not load recent sales</p>}
      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {!isLoading && invoices.length === 0 && <p className="text-sm text-muted-foreground">No sales yet today.</p>}

      <ul className="max-h-60 space-y-2 overflow-y-auto pr-1">
        {invoices.map((invoice) => {
          const voided = invoice.status === 'voided'
          return (
            <li
              key={invoice.id}
              className={cn(
                'flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-border px-3 py-2 text-sm',
                voided && 'opacity-60',
              )}
            >
              <span className="font-medium tabular-nums">{invoice.invoice_number}</span>
              <span className="text-muted-foreground tabular-nums">{timeOf(invoice.created_at)}</span>
              <Badge variant="outline" className="capitalize">
                {invoice.payment_method}
              </Badge>
              {voided && <Badge variant="destructive">Voided</Badge>}
              <span className="ml-auto font-semibold tabular-nums">
                <Money amount={invoice.total_payable} />
              </span>
              <div className="flex w-full items-center justify-end gap-1 sm:w-auto">
                <Button variant="ghost" size="sm" onClick={() => reprint(invoice, 'thermal')}>
                  <Printer className="mr-1 h-3.5 w-3.5" /> Receipt
                </Button>
                <Button variant="ghost" size="sm" onClick={() => reprint(invoice, 'a4')}>
                  A4
                </Button>
                {!voided && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => {
                      setVoidError(null)
                      setVoiding(invoice)
                    }}
                  >
                    <Ban className="mr-1 h-3.5 w-3.5" /> Void
                  </Button>
                )}
              </div>
            </li>
          )
        })}
      </ul>

      <PinEntryModal
        open={!!voiding}
        onOpenChange={(open) => {
          // Esc and outside-click stay inert while the void is in flight.
          if (voidMutation.isPending) return
          if (!open) {
            setVoiding(null)
            setVoidError(null)
          }
        }}
        title={`Void ${voiding?.invoice_number ?? 'invoice'}`}
        description="The whole invoice is voided and excluded from every total. It stays visible, stamped VOID."
        withReason
        reasonLabel="Why is this being voided?"
        reasonPlaceholder="Wrong weight rung, customer changed their mind…"
        submitLabel="Void invoice"
        submitVariant="destructive"
        pending={voidMutation.isPending}
        error={voidError}
        onSubmit={(pin, reason) => {
          if (!voiding) return
          setVoidError(null)
          voidMutation.mutate({ id: voiding.id, pin, reason })
        }}
      />
    </div>
  )
}
