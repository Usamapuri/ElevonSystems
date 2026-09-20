/**
 * Settlement. Picking the tender here is what decides the tax rate
 * (tax_rate_<tender>, falling back to cash for credit), so the choice feeds
 * straight back into the cart rail's live totals — the figure on the Charge
 * button moves when you switch from Cash to Credit, which is correct and is
 * why the tender lives in the page rather than in this dialog.
 *
 * The dialog itself owns only the settlement detail: sub-method and
 * reference for card/online, the note, and which document to print. Charge
 * hands all of it back to the page, which owns the mutation, the
 * client_op_id and the credit-limit PIN retry.
 */
import { useEffect, useState } from 'react'
import { Banknote, CreditCard, Landmark, Smartphone } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { formatMoney } from '@/lib/money'
import { cn } from '@/lib/utils'
import type { Customer, PaymentMethod, PrintDocument } from '@/types'

/** What the page needs to build the POST besides the cart and the customer. */
export interface TenderDetails {
  payment_method: PaymentMethod
  payment_sub_method?: string
  payment_reference?: string
  notes?: string
  document: PrintDocument
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  tender: PaymentMethod
  onTenderChange: (tender: PaymentMethod) => void
  totalPayable: number
  customer: Customer | null
  defaultDocument: PrintDocument
  pending: boolean
  /** Inline message from the last attempt (customer_required, credit_not_allowed,
   * product_not_found and anything else the server names). */
  error: string | null
  onCharge: (details: TenderDetails) => void
}

const TENDERS: { value: PaymentMethod; label: string; icon: typeof Banknote }[] = [
  { value: 'cash', label: 'Cash', icon: Banknote },
  { value: 'card', label: 'Card', icon: CreditCard },
  { value: 'online', label: 'Online', icon: Smartphone },
  { value: 'credit', label: 'Credit', icon: Landmark },
]

/** Sent as payment_sub_method; the column is free text, these are the three
 * an LPG counter in Pakistan actually sees. */
const SUB_METHODS = [
  { value: 'easypaisa', label: 'Easypaisa' },
  { value: 'jazzcash', label: 'JazzCash' },
  { value: 'bank_transfer', label: 'Bank transfer' },
]

export function TenderDialog({
  open,
  onOpenChange,
  tender,
  onTenderChange,
  totalPayable,
  customer,
  defaultDocument,
  pending,
  error,
  onCharge,
}: Props) {
  const [subMethod, setSubMethod] = useState('')
  const [reference, setReference] = useState('')
  const [notes, setNotes] = useState('')
  const [document, setDocument] = useState<PrintDocument>(defaultDocument)

  useEffect(() => {
    if (!open) return
    setSubMethod('')
    setReference('')
    setNotes('')
    setDocument(defaultDocument)
  }, [open, defaultDocument])

  const needsReference = tender === 'card' || tender === 'online'
  const creditWithoutAccount = tender === 'credit' && !customer
  const creditNotAllowed = tender === 'credit' && !!customer && !customer.credit_allowed
  const blocked = creditWithoutAccount || creditNotAllowed

  const charge = () => {
    if (blocked || pending) return
    onCharge({
      payment_method: tender,
      payment_sub_method: needsReference && subMethod ? subMethod : undefined,
      payment_reference: needsReference && reference.trim() ? reference.trim() : undefined,
      notes: notes.trim() || undefined,
      document,
    })
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !pending && onOpenChange(next)}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Take payment</DialogTitle>
          <DialogDescription>
            {customer ? `${customer.name} · ` : 'Walk-in · '}
            {formatMoney(totalPayable)} due
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-2 gap-2">
          {TENDERS.map(({ value, label, icon: Icon }) => (
            <button
              key={value}
              type="button"
              onClick={() => onTenderChange(value)}
              className={cn(
                'flex h-16 items-center justify-center gap-2 rounded-xl border text-base font-medium transition-colors',
                tender === value
                  ? 'border-primary bg-primary/10 text-primary'
                  : 'border-border bg-background hover:bg-accent',
              )}
            >
              <Icon className="h-5 w-5" />
              {label}
            </button>
          ))}
        </div>

        {needsReference && (
          <div className="space-y-1.5">
            <Label>Channel</Label>
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
            <Label htmlFor="tender-reference">Reference</Label>
            <Input
              id="tender-reference"
              maxLength={100}
              placeholder="Last 4 digits, transaction id…"
              value={reference}
              onChange={(e) => setReference(e.target.value)}
            />
          </div>
        )}

        <div className="space-y-1.5">
          <Label htmlFor="tender-notes">Note (optional)</Label>
          <Textarea
            id="tender-notes"
            rows={2}
            maxLength={2000}
            className="min-h-0"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </div>

        <div className="space-y-1.5">
          <Label>Print</Label>
          <div className="flex rounded-md bg-muted p-1">
            {(['thermal', 'a4'] as const).map((d) => (
              <button
                key={d}
                type="button"
                onClick={() => setDocument(d)}
                className={cn(
                  'flex-1 rounded px-3 py-1.5 text-sm font-medium transition-colors',
                  document === d ? 'bg-background shadow-sm' : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {d === 'thermal' ? 'Thermal receipt' : 'A4 invoice'}
              </button>
            ))}
          </div>
        </div>

        {creditWithoutAccount && (
          <p className="text-sm text-destructive">A credit sale needs a customer account — pick one in the cart.</p>
        )}
        {creditNotAllowed && (
          <p className="text-sm text-destructive">{customer?.name} is not set up for credit.</p>
        )}
        {error && !blocked && <p className="text-sm text-destructive">{error}</p>}

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button className="min-w-[10rem]" onClick={charge} disabled={blocked || pending}>
            {pending ? 'Charging…' : `Charge ${formatMoney(totalPayable)}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
