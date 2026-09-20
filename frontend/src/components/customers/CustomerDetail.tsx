import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import apiClient from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Sheet, SheetBody, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { AgeingCard } from '@/components/customers/AgeingCard'
import { ReceiptsList } from '@/components/customers/ReceiptsList'
import { ReceivePaymentDialog } from '@/components/customers/ReceivePaymentDialog'
import { formatMoney } from '@/lib/money'
import type { LedgerEntryType } from '@/types'

interface Props {
  /** Present opens the sheet for that customer; null/undefined closes it. */
  customerId: string | null
  onOpenChange: (open: boolean) => void
}

function entryTypeLabel(t: LedgerEntryType): string {
  switch (t) {
    case 'invoice':
      return 'Invoice'
    case 'invoice_void':
      return 'Invoice void'
    case 'receipt':
      return 'Receipt'
    case 'receipt_void':
      return 'Receipt void'
    case 'adjustment':
      return 'Adjustment'
    default:
      return t
  }
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-PK', { dateStyle: 'medium' })
}

/** Detail sheet for one customer: profile fields, ageing, receipts (with
 * void), receive payment, and the full statement with a running balance.
 * Receive payment is open to any staff (spec §3); void needs the admin PIN.
 * ReceivePaymentDialog is rendered as a sibling of the Sheet, not nested
 * inside it, so its own Dialog root is independent of the sheet's. */
export function CustomerDetail({ customerId, onOpenChange }: Props) {
  const open = !!customerId
  const [payOpen, setPayOpen] = useState(false)

  const { data: customer, isLoading: loadingCustomer, error: customerError } = useQuery({
    queryKey: ['customer', customerId],
    queryFn: async () => {
      const res = await apiClient.getCustomer(customerId as string)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the customer')
      return res.data
    },
    enabled: open,
  })

  const { data: statement, isLoading: loadingStatement, error: statementError } = useQuery({
    queryKey: ['customer-statement', customerId],
    queryFn: async () => {
      const res = await apiClient.getCustomerStatement(customerId as string)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the statement')
      return res.data
    },
    enabled: open,
  })
  const rows = statement ?? []

  return (
    <>
      <Sheet open={open} onOpenChange={(o) => !o && onOpenChange(false)}>
        <SheetContent size="lg">
          <SheetHeader>
            <SheetTitle>{customer?.name ?? 'Customer'}</SheetTitle>
            <SheetDescription>Profile, ageing, receipts and the full ledger statement, oldest entry first.</SheetDescription>
          </SheetHeader>
          <SheetBody className="space-y-6">
            {customerError && <p className="text-sm text-red-600">{customerError instanceof Error ? customerError.message : 'Could not load the customer'}</p>}
            {loadingCustomer && <p className="text-sm text-muted-foreground">Loading…</p>}
            {customer && (
              <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
                <div>
                  <p className="text-xs text-muted-foreground">Balance</p>
                  <p className={`text-lg font-semibold ${customer.balance > 0 ? 'text-amber-600' : ''}`}>{formatMoney(customer.balance)}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Status</p>
                  <div className="flex flex-wrap gap-1">
                    <Badge variant={customer.is_active ? 'default' : 'secondary'}>{customer.is_active ? 'Active' : 'Inactive'}</Badge>
                    {customer.credit_allowed && <Badge variant="outline">Credit allowed</Badge>}
                  </div>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Phone</p>
                  <p className="text-sm">{customer.phone ?? '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Credit limit</p>
                  <p className="text-sm">{customer.credit_limit === null ? 'No limit' : formatMoney(customer.credit_limit)}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">NTN</p>
                  <p className="text-sm">{customer.ntn ?? '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">CNIC</p>
                  <p className="text-sm">{customer.cnic ?? '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Buyer registration type</p>
                  <p className="text-sm">{customer.buyer_registration_type}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Province</p>
                  <p className="text-sm">{customer.province ?? '—'}</p>
                </div>
                <div className="sm:col-span-2">
                  <p className="text-xs text-muted-foreground">Address</p>
                  <p className="text-sm">{customer.address ?? '—'}</p>
                </div>
                {customer.notes && (
                  <div className="sm:col-span-2">
                    <p className="text-xs text-muted-foreground">Notes</p>
                    <p className="text-sm">{customer.notes}</p>
                  </div>
                )}
              </div>
            )}

            {customer && (
              <div className="flex flex-wrap items-center gap-2">
                <Button size="sm" onClick={() => setPayOpen(true)} disabled={!customer.is_active}>
                  Receive payment
                </Button>
                {!customer.is_active && (
                  <span className="text-xs text-muted-foreground">Inactive customers cannot receive new payments.</span>
                )}
              </div>
            )}

            {customer && <AgeingCard customerId={customer.id} enabled={open} />}

            {customer && <ReceiptsList customerId={customer.id} enabled={open} />}

            <div>
              <h3 className="mb-2 text-sm font-semibold">Statement</h3>
              {statementError && <p className="text-sm text-red-600">{statementError instanceof Error ? statementError.message : 'Could not load the statement'}</p>}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Date</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead className="hidden sm:table-cell">Reference</TableHead>
                    <TableHead className="text-right">Debit</TableHead>
                    <TableHead className="text-right">Credit</TableHead>
                    <TableHead className="text-right">Balance</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loadingStatement && (
                    <TableRow>
                      <TableCell colSpan={6} className="text-center text-muted-foreground">Loading…</TableCell>
                    </TableRow>
                  )}
                  {!loadingStatement && rows.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={6} className="text-center text-muted-foreground">No ledger entries yet.</TableCell>
                    </TableRow>
                  )}
                  {rows.map((row) => (
                    <TableRow key={row.id}>
                      <TableCell className="text-muted-foreground">{formatDate(row.business_date)}</TableCell>
                      <TableCell>{entryTypeLabel(row.entry_type)}</TableCell>
                      <TableCell className="hidden sm:table-cell text-muted-foreground">{row.invoice_number ?? row.receipt_number ?? row.note ?? '—'}</TableCell>
                      <TableCell className="text-right">{row.debit ? formatMoney(row.debit) : '—'}</TableCell>
                      <TableCell className="text-right">{row.credit ? formatMoney(row.credit) : '—'}</TableCell>
                      <TableCell className="text-right font-medium">{formatMoney(row.running_balance)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </SheetBody>
        </SheetContent>
      </Sheet>
      {customer && <ReceivePaymentDialog open={payOpen} onOpenChange={setPayOpen} customer={customer} />}
    </>
  )
}
