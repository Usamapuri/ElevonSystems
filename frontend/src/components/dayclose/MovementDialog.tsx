/**
 * Money in or out of the drawer during the day — a supplier paid in cash, a
 * float top-up, change fetched from the bank.
 *
 * Ported from the retail counter's CashMovementDialog, minus the expense
 * category taxonomy (there is no expense module here) and its optional-notes
 * toggle. The reason is mandatory, because a paid-out with no reason is
 * indistinguishable from a shortage at close, and it is the reason that turns
 * a variance into an explanation.
 *
 * Amount and reason are the server's rules to the letter: more than zero, at
 * most 2 dp, reason 1–200 characters (handlers/dayops.go AddMovement).
 */
import { useEffect, useState } from 'react'
import { ArrowDownToLine, ArrowUpFromLine, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { NumericKeypad } from '@/components/shared/NumericKeypad'
import { cn } from '@/lib/utils'
import { formatMoney } from '@/lib/money'
import { parseMovementAmount } from '@/components/dayclose/dayClose'

export type MovementType = 'paid_in' | 'paid_out'

/** Matches the server's `len(reason) > 200` refusal. */
const REASON_MAX = 200

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Which button opened it; the person can still switch inside. */
  defaultType: MovementType
  pending: boolean
  error: string | null
  onSubmit: (movement: { type: MovementType; amount: number; reason: string; notes: string }) => void
}

export function MovementDialog({ open, onOpenChange, defaultType, pending, error, onSubmit }: Props) {
  const [type, setType] = useState<MovementType>(defaultType)
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  const [notes, setNotes] = useState('')

  useEffect(() => {
    if (!open) return
    setType(defaultType)
    setAmount('')
    setReason('')
    setNotes('')
  }, [open, defaultType])

  const value = parseMovementAmount(amount)
  const trimmedReason = reason.trim()
  const canSubmit = value !== null && trimmedReason !== '' && !pending

  const submit = () => {
    if (value === null || trimmedReason === '' || pending) return
    onSubmit({ type, amount: value, reason: trimmedReason, notes: notes.trim() })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (pending) return
        onOpenChange(next)
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Cash movement</DialogTitle>
          <DialogDescription>
            Money that moved in or out of the drawer without a sale. It changes what the drawer should hold at close, so
            record it as it happens.
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-2 gap-2">
          <Button
            type="button"
            variant={type === 'paid_in' ? 'default' : 'outline'}
            className={cn('h-12', type === 'paid_in' && 'ring-2 ring-ring')}
            onClick={() => setType('paid_in')}
          >
            <ArrowDownToLine className="mr-2 h-4 w-4" /> Paid in
          </Button>
          <Button
            type="button"
            variant={type === 'paid_out' ? 'default' : 'outline'}
            className={cn('h-12', type === 'paid_out' && 'ring-2 ring-ring')}
            onClick={() => setType('paid_out')}
          >
            <ArrowUpFromLine className="mr-2 h-4 w-4" /> Paid out
          </Button>
        </div>

        <div className="space-y-2">
          <Label htmlFor="movement-amount">Amount (Rs)</Label>
          <Input
            id="movement-amount"
            inputMode="decimal"
            autoComplete="off"
            value={amount}
            onChange={(e) => setAmount(e.target.value.replace(/[^0-9.]/g, ''))}
            className="h-14 text-center text-3xl font-semibold tabular"
          />
          <p className="h-5 text-center text-sm text-muted-foreground">{value === null ? '' : formatMoney(value)}</p>
          <NumericKeypad value={amount} onChange={setAmount} maxDecimals={2} maxLength={11} />
        </div>

        <div className="space-y-2">
          <Label htmlFor="movement-reason">Reason</Label>
          <Input
            id="movement-reason"
            maxLength={REASON_MAX}
            value={reason}
            placeholder={type === 'paid_in' ? 'Float top-up, change from the bank…' : 'Supplier paid in cash, fuel…'}
            onChange={(e) => setReason(e.target.value)}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="movement-notes">Notes (optional)</Label>
          <Textarea id="movement-notes" rows={2} maxLength={500} value={notes} onChange={(e) => setNotes(e.target.value)} />
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!canSubmit}>
            {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {type === 'paid_in' ? 'Record money in' : 'Record money out'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
