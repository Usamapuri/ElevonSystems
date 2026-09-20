/**
 * One invoice, in full: the lines as they were rung, the totals as the server
 * computed them, and the two things a counter needs after the fact — print it
 * again, and void it.
 *
 * A listing carries no `lines`, so the drawer re-reads the invoice by id.
 * That read is also what a reprint prints: the receipt must come off the
 * stored invoice, never off a row that has been sitting in a query cache
 * since before someone voided it.
 *
 * A void is whole-invoice, needs a written reason and an admin PIN, and
 * leaves the invoice exactly where it is with status `voided` — still
 * listed, still printable (stamped VOID), excluded from every total (spec
 * §6.4). The button is gone once the invoice is already voided: `POST
 * /invoices/:id/void` answers `invoice_already_voided` and there is nothing
 * for a second void to do.
 */
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Ban, Loader2, Printer } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Sheet, SheetBody, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { PinEntryModal } from '@/components/shared/PinEntryModal'
import { toast } from '@/hooks/use-toast'
import { formatKg, formatMoney } from '@/lib/money'
import {
  formatBusinessDate,
  formatDateTimePK,
  paymentMethodLabel,
  percentLabel,
  plainPercentLabel,
  signedAmount2,
  subMethodLabel,
} from '@/lib/print/format'
import { printInvoice } from '@/lib/print/printInvoice'
import { cn } from '@/lib/utils'
import type { EnteredAs, PrintDocument } from '@/types'

interface Props {
  /** Present opens the drawer for that invoice; null closes it. */
  invoiceId: string | null
  onOpenChange: (open: boolean) => void
}

function enteredAsLabel(entered: EnteredAs | null): string {
  switch (entered) {
    case 'kg':
      return 'kg'
    case 'tonne':
      return 'tonne'
    case 'amount':
      return 'by amount'
    case 'gross_tare':
      return 'gross − tare'
    default:
      return ''
  }
}

export function InvoiceDrawer({ invoiceId, onOpenChange }: Props) {
  const qc = useQueryClient()
  const open = !!invoiceId
  const [voidOpen, setVoidOpen] = useState(false)
  const [voidError, setVoidError] = useState<string | null>(null)
  const [printing, setPrinting] = useState(false)

  const { data: invoice, isLoading, error } = useQuery({
    queryKey: ['invoice', invoiceId],
    queryFn: async () => {
      const res = await apiClient.getInvoice(invoiceId as string)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the invoice')
      return res.data
    },
    enabled: open,
  })

  const voided = invoice?.status === 'voided'

  const reprint = async (document: PrintDocument) => {
    if (!invoice) return
    setPrinting(true)
    try {
      await printInvoice(invoice, document)
    } catch (err) {
      toast({
        title: 'Could not reprint',
        description: err instanceof Error ? err.message : 'Unknown error',
        variant: 'destructive',
      })
    } finally {
      setPrinting(false)
    }
  }

  const voidMutation = useMutation({
    mutationFn: ({ pin, reason }: { pin: string; reason: string }) =>
      apiClient.voidInvoice(invoiceId as string, { pin, reason }),
    onSuccess: (res) => {
      setVoidOpen(false)
      setVoidError(null)
      // ['invoices'] covers both the browser's list and the till's recent strip.
      qc.invalidateQueries({ queryKey: ['invoices'] })
      qc.invalidateQueries({ queryKey: ['invoice', invoiceId] })
      qc.invalidateQueries({ queryKey: ['day', 'current'] })
      qc.invalidateQueries({ queryKey: ['customers'] })
      toast({ title: `Invoice ${res.data?.invoice_number ?? ''} voided`, variant: 'success' })
    },
    onError: (err: unknown) => {
      if (err instanceof ApiClientError && err.code === 'invoice_already_voided') {
        setVoidOpen(false)
        setVoidError(null)
        qc.invalidateQueries({ queryKey: ['invoices'] })
        qc.invalidateQueries({ queryKey: ['invoice', invoiceId] })
        toast({ title: 'That invoice was already voided', variant: 'destructive' })
        return
      }
      setVoidError(err instanceof Error ? err.message : 'Could not void the invoice')
    },
  })

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (voidMutation.isPending) return
        if (!next) onOpenChange(false)
      }}
    >
      <SheetContent size="lg">
        <SheetHeader>
          <SheetTitle className="flex flex-wrap items-center gap-2">
            {invoice?.invoice_number ?? 'Invoice'}
            {voided && <Badge variant="destructive">Voided</Badge>}
          </SheetTitle>
          <SheetDescription>
            {invoice
              ? `${formatBusinessDate(invoice.business_date)} · rung by ${invoice.cashier_name} at ${formatDateTimePK(invoice.created_at)}`
              : 'Loading…'}
          </SheetDescription>
        </SheetHeader>

        <SheetBody className="space-y-6">
          {error && <p className="text-sm text-destructive">{error instanceof Error ? error.message : 'Could not load the invoice'}</p>}
          {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}

          {invoice && (
            <>
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" disabled={printing} onClick={() => reprint('thermal')}>
                  <Printer className="mr-2 h-4 w-4" /> Receipt
                </Button>
                <Button variant="outline" size="sm" disabled={printing} onClick={() => reprint('a4')}>
                  <Printer className="mr-2 h-4 w-4" /> A4 invoice
                </Button>
                {!voided && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => {
                      setVoidError(null)
                      setVoidOpen(true)
                    }}
                  >
                    <Ban className="mr-2 h-4 w-4" /> Void
                  </Button>
                )}
                {printing && <Loader2 className="mt-2 h-4 w-4 animate-spin text-muted-foreground" />}
              </div>

              {voided && (
                <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">
                  <p className="font-medium">This invoice is voided and excluded from every total.</p>
                  {invoice.void_reason && <p className="text-muted-foreground">Reason: {invoice.void_reason}</p>}
                  {invoice.voided_at && (
                    <p className="text-muted-foreground">Voided {formatDateTimePK(invoice.voided_at)}</p>
                  )}
                </div>
              )}

              <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
                <Field label="Customer" value={invoice.customer_name ?? 'Walk-in'} />
                <Field
                  label="Tender"
                  value={
                    paymentMethodLabel(invoice.payment_method) +
                    (invoice.payment_sub_method ? ` · ${subMethodLabel(invoice.payment_sub_method)}` : '')
                  }
                />
                <Field label="Phone" value={invoice.customer_phone ?? '—'} />
                <Field label="Reference" value={invoice.payment_reference ?? '—'} />
                <Field label="NTN" value={invoice.customer_ntn ?? '—'} />
                <Field label="CNIC" value={invoice.customer_cnic ?? '—'} />
                {invoice.notes && (
                  <div className="sm:col-span-2">
                    <p className="text-xs text-muted-foreground">Notes</p>
                    <p className="whitespace-pre-wrap text-sm">{invoice.notes}</p>
                  </div>
                )}
              </div>

              <div>
                <h3 className="mb-2 text-sm font-semibold">Lines</h3>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Product</TableHead>
                      <TableHead className="text-right">Quantity</TableHead>
                      <TableHead className="text-right">Rate</TableHead>
                      <TableHead className="text-right">Line total</TableHead>
                      <TableHead className="hidden sm:table-cell text-right">Tax</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(invoice.lines ?? []).length === 0 && (
                      <TableRow>
                        <TableCell colSpan={5} className="text-center text-muted-foreground">
                          No lines on this invoice.
                        </TableCell>
                      </TableRow>
                    )}
                    {(invoice.lines ?? []).map((line) => (
                      <TableRow key={line.id}>
                        <TableCell>
                          <span className="font-medium">{line.product_name}</span>
                          {line.entered_as && (
                            <span className="block text-xs text-muted-foreground">
                              rung {enteredAsLabel(line.entered_as)}
                              {line.entered_as === 'gross_tare' && line.gross_weight !== null && line.tare_weight !== null
                                ? ` · ${formatKg(line.gross_weight)} − ${formatKg(line.tare_weight)}`
                                : ''}
                            </span>
                          )}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">{formatKg(line.quantity)}</TableCell>
                        <TableCell className="text-right tabular-nums">{formatMoney(line.unit_price)}</TableCell>
                        <TableCell className="text-right tabular-nums">{formatMoney(line.line_total)}</TableCell>
                        <TableCell className="hidden sm:table-cell text-right tabular-nums">
                          {formatMoney(line.line_tax)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>

              <div>
                <h3 className="mb-2 text-sm font-semibold">Totals</h3>
                <dl className="space-y-1 text-sm">
                  <Line label="Subtotal" value={formatMoney(invoice.subtotal)} />
                  <Line
                    label={
                      invoice.discount_percent
                        ? `Discount (${plainPercentLabel(invoice.discount_percent)})`
                        : 'Discount'
                    }
                    value={`-${formatMoney(invoice.discount_amount)}`}
                  />
                  <Line label={`Tax (${percentLabel(invoice.tax_rate)})`} value={formatMoney(invoice.tax_amount)} />
                  {invoice.further_tax_amount > 0 && (
                    <Line label="Further tax" value={formatMoney(invoice.further_tax_amount)} />
                  )}
                  <Line label="Total" value={formatMoney(invoice.total_amount)} />
                  <Line label="Rounding" value={`Rs ${signedAmount2(invoice.rounding_adjustment)}`} />
                  <Line label="Total payable" value={formatMoney(invoice.total_payable)} strong />
                  {invoice.customer_balance_after !== null && (
                    <Line label="Customer balance after" value={formatMoney(invoice.customer_balance_after)} />
                  )}
                </dl>
              </div>
            </>
          )}
        </SheetBody>
      </SheetContent>

      <PinEntryModal
        open={voidOpen}
        onOpenChange={(next) => {
          // Esc and outside-click stay inert while the void is in flight.
          if (voidMutation.isPending) return
          setVoidOpen(next)
          if (!next) setVoidError(null)
        }}
        title={`Void ${invoice?.invoice_number ?? 'invoice'}`}
        description="The whole invoice is voided and excluded from every total. It stays visible, stamped VOID."
        withReason
        reasonLabel="Why is this being voided?"
        reasonPlaceholder="Wrong weight rung, customer changed their mind…"
        submitLabel="Void invoice"
        submitVariant="destructive"
        pending={voidMutation.isPending}
        error={voidError}
        onSubmit={(pin, reason) => {
          setVoidError(null)
          voidMutation.mutate({ pin, reason })
        }}
      />
    </Sheet>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="text-sm">{value}</p>
    </div>
  )
}

function Line({ label, value, strong = false }: { label: string; value: string; strong?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn('tabular-nums', strong && 'text-base font-semibold')}>{value}</dd>
    </div>
  )
}
