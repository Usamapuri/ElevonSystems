/**
 * Declaring the float that starts the day (spec §6.7).
 *
 * Ported from the retail counter's OpenDayDialog and cut back hard: no
 * standing-float prefill, no counter-tier "confirm only" mode, no staging.
 * One till, one person on it, and nobody has counted this drawer but them.
 *
 * Two things the retail version learnt the hard way and this keeps:
 *
 *  - There is **no `placeholder="0"`**. A grey zero on a required money box
 *    reads as an entered value, so the person moves on believing they have
 *    declared a float they never typed.
 *  - Nothing is suggested. Expected cash at close is `opening_float + cash
 *    sales + paid in − paid out`, so an understated float is self-concealing:
 *    declare Rs 0 on a drawer holding Rs 10,000 and exactly that much can
 *    walk with the day still reconciling to the paisa.
 */
import { useEffect, useState } from 'react'
import { Loader2, Sunrise } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { NumericKeypad } from '@/components/shared/NumericKeypad'
import { formatMoney } from '@/lib/money'
import { parseOpeningCash } from '@/components/dayclose/dayClose'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  pending: boolean
  /** The server's own wording from the last attempt, shown inline. */
  error: string | null
  onSubmit: (opening_cash: number, notes: string) => void
}

export function OpenDayDialog({ open, onOpenChange, pending, error, onSubmit }: Props) {
  const [cash, setCash] = useState('')
  const [notes, setNotes] = useState('')

  // A fresh open is a fresh declaration: never carry a float across two days.
  useEffect(() => {
    if (!open) return
    setCash('')
    setNotes('')
  }, [open])

  const amount = parseOpeningCash(cash)
  const canSubmit = amount !== null && !pending

  const submit = () => {
    if (amount === null || pending) return
    onSubmit(amount, notes.trim())
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // Esc and outside-click stay inert while the POST is in flight.
        if (pending) return
        onOpenChange(next)
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Sunrise className="h-5 w-5" /> Start the day
          </DialogTitle>
          <DialogDescription>
            Count what is physically in the drawer right now. Every cash figure until close is measured against this
            number, so it has to be the real one — zero is fine if the drawer is empty.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="opening-cash">Cash in the drawer (Rs)</Label>
          <Input
            id="opening-cash"
            inputMode="decimal"
            autoComplete="off"
            value={cash}
            onChange={(e) => setCash(e.target.value.replace(/[^0-9.]/g, ''))}
            className="h-14 text-center text-3xl font-semibold tabular-nums"
          />
          <p className="h-5 text-center text-sm text-muted-foreground">
            {amount === null ? '' : formatMoney(amount)}
          </p>
          <NumericKeypad value={cash} onChange={setCash} maxDecimals={2} maxLength={11} />
        </div>

        <div className="space-y-2">
          <Label htmlFor="opening-notes">Notes (optional)</Label>
          <Textarea
            id="opening-notes"
            rows={2}
            maxLength={500}
            value={notes}
            placeholder="Anything worth remembering about this drawer"
            onChange={(e) => setNotes(e.target.value)}
          />
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!canSubmit}>
            {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Start the day
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
