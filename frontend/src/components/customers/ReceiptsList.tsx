/**
 * A customer's receipts, newest first (spec §6.5) — the account screen's
 * "payments taken" panel. Void needs the admin PIN: PinEntryModal collects
 * it and a written reason (server: 4–500 chars), and this component owns
 * the void mutation the same way the till owns an invoice void.
 */
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import apiClient, { ApiClientError } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { PinEntryModal } from '@/components/shared/PinEntryModal'
import { formatMoney } from '@/lib/money'
import { formatBusinessDate, paymentMethodLabel, subMethodLabel } from '@/lib/print/format'
import type { Receipt } from '@/types'

interface Props {
  customerId: string
  /** Only fetches while the sheet holding this list is actually open. */
  enabled: boolean
}

export function ReceiptsList({ customerId, enabled }: Props) {
  const qc = useQueryClient()
  const [voiding, setVoiding] = useState<Receipt | null>(null)
  const [pinError, setPinError] = useState<string | null>(null)
  const [listError, setListError] = useState<string | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['receipts', customerId],
    queryFn: async () => {
      const res = await apiClient.getReceipts(customerId)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load receipts')
      return res.data
    },
    enabled,
  })
  const receipts = data ?? []

  const voidMutation = useMutation({
    mutationFn: ({ pin, reason }: { pin: string; reason: string }) => {
      if (!voiding) throw new Error('No receipt selected')
      return apiClient.voidReceipt(customerId, voiding.id, { pin, reason })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['customers'] })
      qc.invalidateQueries({ queryKey: ['customer', customerId] })
      qc.invalidateQueries({ queryKey: ['customer-statement', customerId] })
      qc.invalidateQueries({ queryKey: ['receipts', customerId] })
      qc.invalidateQueries({ queryKey: ['ageing', customerId] })
      setVoiding(null)
      setPinError(null)
      setListError(null)
    },
    onError: (err: unknown) => {
      const code = err instanceof ApiClientError ? err.code : undefined
      const message = err instanceof Error ? err.message : 'Could not void the receipt'
      if (code === 'invalid_pin') {
        setPinError(message)
        return
      }
      // receipt_already_voided or anything else: the PIN itself was fine —
      // close the modal and say why against the list instead of shaking a
      // PIN box that was never the problem.
      setVoiding(null)
      setPinError(null)
      setListError(message)
    },
  })

  return (
    <div>
      <h3 className="mb-2 text-sm font-semibold">Receipts</h3>
      {(error || listError) && (
        <p className="mb-2 text-sm text-red-600">
          {listError ?? (error instanceof Error ? error.message : 'Could not load receipts')}
        </p>
      )}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Number</TableHead>
            <TableHead>Date</TableHead>
            <TableHead>Method</TableHead>
            <TableHead className="text-right">Amount</TableHead>
            <TableHead className="text-right">Void</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading && (
            <TableRow>
              <TableCell colSpan={5} className="text-center text-muted-foreground">Loading…</TableCell>
            </TableRow>
          )}
          {!isLoading && receipts.length === 0 && (
            <TableRow>
              <TableCell colSpan={5} className="text-center text-muted-foreground">No receipts yet.</TableCell>
            </TableRow>
          )}
          {receipts.map((r) => (
            <TableRow key={r.id} className={r.voided_at ? 'opacity-60' : undefined}>
              <TableCell className="font-medium">{r.receipt_number}</TableCell>
              <TableCell className="text-muted-foreground">{formatBusinessDate(r.business_date)}</TableCell>
              <TableCell>
                {paymentMethodLabel(r.method)}
                {r.sub_method && <span className="text-muted-foreground"> · {subMethodLabel(r.sub_method)}</span>}
              </TableCell>
              <TableCell className="text-right">{formatMoney(r.amount)}</TableCell>
              <TableCell className="text-right">
                {r.voided_at ? (
                  <Badge variant="secondary">Voided</Badge>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setPinError(null)
                      setListError(null)
                      setVoiding(r)
                    }}
                  >
                    Void
                  </Button>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <PinEntryModal
        open={!!voiding}
        onOpenChange={(nextOpen) => {
          if (voidMutation.isPending) return
          if (!nextOpen) {
            setVoiding(null)
            setPinError(null)
          }
        }}
        title={voiding ? `Void receipt ${voiding.receipt_number}` : 'Void receipt'}
        description="This puts the money back on the customer's account. Requires an admin PIN."
        withReason
        reasonLabel="Reason for the void"
        submitLabel="Void receipt"
        submitVariant="destructive"
        pending={voidMutation.isPending}
        error={pinError}
        onSubmit={(pin, reason) => {
          setPinError(null)
          voidMutation.mutate({ pin, reason })
        }}
      />
    </div>
  )
}
