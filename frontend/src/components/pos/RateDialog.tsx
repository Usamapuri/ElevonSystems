/**
 * The deal-rate field for one cart line (spec 2026-10-12 §3). The cashier
 * types the rate the deal was struck at; the dialog shows what the line
 * comes to and whether the server will want an admin PIN for it. Nothing
 * here is money the server trusts — it re-prices from the rate and checks
 * the band itself.
 */
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatMoney } from '@/lib/money'
import { round2 } from '@/lib/pricing'
import { formatKg } from '@/lib/weight'
import type { CartLine } from './cart'
import { effectiveRate, needsPin, parseRateInput } from './dealRate'

interface Props {
  line: CartLine | null
  /** settings.rate_override_band — Rs per kg allowed without a PIN. */
  band: number
  open: boolean
  onOpenChange: (open: boolean) => void
  /** null = back to the list rate. */
  onConfirm: (rate: number | null) => void
}

export function RateDialog({ line, band, open, onOpenChange, onConfirm }: Props) {
  const [text, setText] = useState('')

  useEffect(() => {
    if (open && line) setText(effectiveRate(line).toFixed(2))
  }, [open, line])

  if (!line) return null
  const rate = parseRateInput(text)
  const lineTotal = rate === null ? null : round2(rate * line.quantity)
  const pin = rate !== null && needsPin(line.rate, rate, band)
  const isList = rate !== null && Math.round(rate * 100) === Math.round(line.rate * 100)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Rate for this line</DialogTitle>
          <DialogDescription>
            {line.product_name} — list rate <span className="tabular">{formatMoney(line.rate)}</span> per kg
          </DialogDescription>
        </DialogHeader>

        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (rate === null) return
            onConfirm(isList ? null : rate)
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="deal-rate">Rate per kg (Rs)</Label>
            <Input
              id="deal-rate"
              inputMode="decimal"
              autoFocus
              className="tabular text-lg"
              value={text}
              onChange={(e) => setText(e.target.value)}
              onFocus={(e) => e.target.select()}
            />
            {rate === null && text.trim() !== '' && (
              <p className="text-xs font-semibold text-destructive">Enter a rate above zero with at most 2 decimal places</p>
            )}
          </div>
          <p className="tabular text-sm text-muted-foreground">
            {rate === null ? '—' : <>{formatMoney(rate)} × {formatKg(line.quantity)} kg = {formatMoney(lineTotal ?? 0)}</>}
          </p>
          {pin && <p className="text-xs font-semibold text-muted-foreground">Outside the allowed band — needs an admin PIN at Charge.</p>}
          <DialogFooter className="gap-2 sm:gap-2">
            <Button type="button" variant="outline" onClick={() => onConfirm(null)}>
              Use list rate
            </Button>
            <Button type="submit" disabled={rate === null}>
              Set rate
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
