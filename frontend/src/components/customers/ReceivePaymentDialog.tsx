/**
 * Receive Payment (spec §6.5, §3): cash, card or online taken against a
 * customer's account, not against a specific invoice. Any staff may open
 * this — the counter takes payments same as an admin; only voiding one
 * needs the admin PIN (see ReceiptsList).
 *
 * CreateReceipt goes through the same day gate as a sale
 * (dayops.EnsureOpenDayForInvoice): a 409 `day_not_open` or
 * `previous_day_open` comes back here as the same banner the till shows,
 * with the same link to /day-close, via gateFromErrorCode.
 */
import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { NumericKeypad } from '@/components/shared/NumericKeypad'
import { DayGateBanner } from '@/components/pos/DayGateBanner'
import { gateFromErrorCode, type DayGate } from '@/components/pos/dayGate'
import { toast } from '@/hooks/use-toast'
import { formatMoney } from '@/lib/money'
import { parseReceiptAmount, previewBalanceAfter } from './receivePayment'
import type { Customer, CreateReceiptRequest, ReceiptMethod } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  customer: Customer
}

const METHODS: { value: ReceiptMethod; label: string }[] = [
  { value: 'cash', label: 'Cash' },
  { value: 'card', label: 'Card' },
  { value: 'online', label: 'Online' },
]

/** Sent as sub_method; the column is free text, these are the three an LPG
 * counter in Pakistan actually sees (matches TenderDialog's list). */
const SUB_METHODS = [
  { value: 'easypaisa', label: 'Easypaisa' },
  { value: 'jazzcash', label: 'JazzCash' },
  { value: 'bank_transfer', label: 'Bank transfer' },
]

export function ReceivePaymentDialog({ open, onOpenChange, customer }: Props) {
  const qc = useQueryClient()
  const [amount, setAmount] = useState('')
  const [method, setMethod] = useState<ReceiptMethod>('cash')
  const [subMethod, setSubMethod] = useState('')
  const [reference, setReference] = useState('')
  const [note, setNote] = useState('')
  const [dayGate, setDayGate] = useState<DayGate | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setAmount('')
    setMethod('cash')
    setSubMethod('')
    setReference('')
    setNote('')
    setDayGate(null)
    setError(null)
  }, [open])

  const parsedAmount = parseReceiptAmount(amount)
  const balanceAfter = previewBalanceAfter(customer.balance, parsedAmount)
  const needsReference = method === 'card' || method === 'online'

  const mutation = useMutation({
    mutationFn: () => {
      if (parsedAmount === null) throw new Error('Enter an amount more than zero, at most 2 decimal places')
      const req: CreateReceiptRequest = {
        amount: parsedAmount,
        method,
        sub_method: needsReference && subMethod ? subMethod : undefined,
        reference: needsReference && reference.trim() ? reference.trim() : undefined,
        note: note.trim() || undefined,
      }
      return apiClient.createReceipt(customer.id, req)
    },
    onSuccess: (res) => {
      const receipt = res.data
      qc.invalidateQueries({ queryKey: ['customers'] })
      qc.invalidateQueries({ queryKey: ['customer', customer.id] })
      qc.invalidateQueries({ queryKey: ['customer-statement', customer.id] })
      qc.invalidateQueries({ queryKey: ['receipts', customer.id] })
      qc.invalidateQueries({ queryKey: ['ageing', customer.id] })
      toast({
        title: receipt ? `Receipt ${receipt.receipt_number}` : 'Receipt recorded',
        description:
          receipt?.customer_balance_after != null ? `Balance now ${formatMoney(receipt.customer_balance_after)}` : undefined,
        variant: 'success',
      })
      onOpenChange(false)
    },
    onError: (err: unknown) => {
      const code = err instanceof ApiClientError ? err.code : undefined
      const message = err instanceof Error ? err.message : 'Could not record the payment'
      const gate = gateFromErrorCode(code)
      if (gate) {
        setDayGate(gate)
        setError(null)
        return
      }
      setDayGate(null)
      setError(message)
    },
  })

  const canSubmit = parsedAmount !== null && !mutation.isPending

  const submit = () => {
    if (!canSubmit) return
    setError(null)
    mutation.mutate()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // Never release the dialog mid-POST — the mutation only settles once,
        // and closing early would let a second press mint a second receipt.
        if (mutation.isPending) return
        onOpenChange(next)
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Receive payment</DialogTitle>
          <DialogDescription>
            {customer.name} · current balance {formatMoney(customer.balance)}
          </DialogDescription>
        </DialogHeader>

        {dayGate && <DayGateBanner gate={dayGate} />}

        <div className="grid grid-cols-3 gap-2">
          {METHODS.map(({ value, label }) => (
            <Button
              key={value}
              type="button"
              variant={method === value ? 'default' : 'outline'}
              className="h-12"
              onClick={() => setMethod(value)}
              disabled={mutation.isPending}
            >
              {label}
            </Button>
          ))}
        </div>

        <div className="space-y-2">
          <Label htmlFor="receipt-amount">Amount (Rs)</Label>
          <Input
            id="receipt-amount"
            inputMode="decimal"
            autoComplete="off"
            value={amount}
            onChange={(e) => setAmount(e.target.value.replace(/[^0-9.]/g, ''))}
            className="h-14 text-center text-3xl font-semibold tabular-nums"
            disabled={mutation.isPending}
          />
          <p className="h-5 text-center text-sm text-muted-foreground">
            {balanceAfter === null ? '' : `Balance after: ${formatMoney(balanceAfter)}`}
          </p>
          <NumericKeypad value={amount} onChange={setAmount} maxDecimals={2} maxLength={11} />
        </div>

        {needsReference && (
          <div className="space-y-1.5">
            <Label>Channel (optional)</Label>
            <Select value={subMethod} onValueChange={setSubMethod}>
              <SelectTrigger>
                <SelectValue placeholder="Easypaisa, JazzCash, bank transfer…" />
              </SelectTrigger>
              <SelectContent>
                {SUB_METHODS.map((m) => (
                  <SelectItem key={m.value} value={m.value}>
                    {m.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        {needsReference && (
          <div className="space-y-1.5">
            <Label htmlFor="receipt-reference">Reference (optional)</Label>
            <Input
              id="receipt-reference"
              maxLength={100}
              placeholder="Last 4 digits, transaction id…"
              value={reference}
              onChange={(e) => setReference(e.target.value)}
            />
          </div>
        )}

        <div className="space-y-1.5">
          <Label htmlFor="receipt-note">Note (optional)</Label>
          <Textarea
            id="receipt-note"
            rows={2}
            maxLength={2000}
            className="min-h-0"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button className="min-w-[10rem]" onClick={submit} disabled={!canSubmit}>
            {mutation.isPending ? 'Recording…' : 'Record payment'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
