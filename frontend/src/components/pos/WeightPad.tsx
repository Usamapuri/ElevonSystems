/**
 * The weight pad (spec §6.2). One dialog, four ways to say the same thing:
 *
 *   kg           — straight off the scale
 *   tonne        — a bulk load, ×1000
 *   amount       — "give me Rs 3,000 of gas", ÷ rate
 *   gross − tare — the scale reading minus the cylinder
 *
 * All four resolve to net kg to 3 dp before anything leaves this component
 * (lib/weight.toLineInput), and the mode travels with the line as
 * `entered_as` so a reprint can show the sale the way it was rung.
 *
 * The live preview uses lib/pricing.round2 — the same half-up paisa rounding
 * the server uses for line_total — so the number on screen is the number the
 * server will compute for that line. It is still only a preview: the charge
 * is priced server-side from products.rate.
 */
import { useEffect, useMemo, useState } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NumericKeypad } from '@/components/shared/NumericKeypad'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { formatMoney } from '@/lib/money'
import { round2 } from '@/lib/pricing'
import { cn } from '@/lib/utils'
import { WEIGHT_MODES, formatKg, isLineInputError, modeLabel, toLineInput, type WeightMode } from '@/lib/weight'
import type { Product } from '@/types'
import type { CartLine, CartLineDraft } from './cart'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  product: Product | null
  /** Set when a cart line was tapped: the pad opens on that line's values
   * and Confirm updates it instead of adding another. */
  editing: CartLine | null
  onConfirm: (line: CartLineDraft) => void
  onRemove?: () => void
}

export function WeightPad({ open, onOpenChange, product, editing, onConfirm, onRemove }: Props) {
  const coarsePointer = useMediaQuery('(pointer: coarse)')
  const [mode, setMode] = useState<WeightMode>('kg')
  const [value, setValue] = useState('')
  const [gross, setGross] = useState('')
  const [tare, setTare] = useState('')
  const [tareFocused, setTareFocused] = useState(false)
  const [keypad, setKeypad] = useState(false)
  const [touched, setTouched] = useState(false)

  const rate = product?.rate ?? 0

  // Every open is a fresh entry, or the line being edited put back exactly
  // as it was rung — including the rupee figure the customer named in
  // amount mode, which is not recoverable from the quantity alone.
  useEffect(() => {
    if (!open) return
    setKeypad(coarsePointer)
    setTouched(false)
    setTareFocused(false)
    if (editing) {
      setMode(editing.entered_as)
      setGross(editing.gross_weight !== undefined ? formatKg(editing.gross_weight) : '')
      setTare(editing.tare_weight !== undefined ? formatKg(editing.tare_weight) : '')
      if (editing.entered_as === 'amount') setValue(editing.typed_amount !== undefined ? String(editing.typed_amount) : '')
      else if (editing.entered_as === 'tonne') setValue(String(editing.quantity / 1000))
      else setValue(formatKg(editing.quantity))
      return
    }
    setMode('kg')
    setValue('')
    setGross('')
    setTare('')
  }, [open, editing, coarsePointer])

  const result = useMemo(
    () => toLineInput(mode, { value, gross, tare }, rate),
    [mode, value, gross, tare, rate],
  )
  const line = isLineInputError(result) ? null : result
  const error = isLineInputError(result) ? result.error : null
  const lineTotal = line ? round2(line.quantity * rate) : 0
  const typedAmount = mode === 'amount' ? Number(value.trim()) : NaN

  const switchMode = (next: WeightMode) => {
    // A figure typed as kg is not the same figure in tonnes or rupees;
    // start the new mode empty rather than silently reinterpreting it.
    setMode(next)
    setValue('')
    setGross('')
    setTare('')
    setTouched(false)
    setTareFocused(false)
  }

  const confirm = () => {
    setTouched(true)
    if (!line || !product) return
    onConfirm({
      product_id: product.id,
      product_name: product.name,
      rate: product.rate,
      quantity: line.quantity,
      entered_as: line.entered_as,
      gross_weight: line.gross_weight,
      tare_weight: line.tare_weight,
      typed_amount: mode === 'amount' && Number.isFinite(typedAmount) ? typedAmount : undefined,
    })
    onOpenChange(false)
  }

  // In gross − tare mode the keypad drives whichever of the two fields the
  // cashier last touched; everywhere else there is only one field.
  const keypadValue = mode === 'gross_tare' ? (tareFocused ? tare : gross) : value
  const setKeypadValue = (next: string) => {
    if (mode !== 'gross_tare') setValue(next)
    else if (tareFocused) setTare(next)
    else setGross(next)
  }

  const bigInputProps = {
    inputMode: 'decimal' as const,
    autoComplete: 'off',
    className: 'h-14 text-2xl font-semibold tabular-nums',
    onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter') {
        e.preventDefault()
        confirm()
      }
    },
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{product?.name ?? 'Weight'}</DialogTitle>
          <DialogDescription>
            {rate > 0
              ? `${formatMoney(rate)} per ${product?.unit_label ?? 'kg'}`
              : 'This product has no rate — set it on the Rates screen first.'}
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-4 gap-1 rounded-lg bg-muted p-1">
          {WEIGHT_MODES.map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => switchMode(m)}
              className={cn(
                'rounded-md px-2 py-2 text-xs font-medium transition-colors sm:text-sm',
                mode === m ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {modeLabel(m)}
            </button>
          ))}
        </div>

        {mode === 'gross_tare' ? (
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="pad-gross">Gross (kg)</Label>
              <Input
                id="pad-gross"
                autoFocus
                value={gross}
                onFocus={() => setTareFocused(false)}
                onChange={(e) => {
                  setGross(e.target.value)
                  setTouched(true)
                }}
                {...bigInputProps}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pad-tare">Tare (kg)</Label>
              <Input
                id="pad-tare"
                value={tare}
                onFocus={() => setTareFocused(true)}
                onChange={(e) => {
                  setTare(e.target.value)
                  setTouched(true)
                }}
                {...bigInputProps}
              />
            </div>
          </div>
        ) : (
          <div className="space-y-1.5">
            <Label htmlFor="pad-value">
              {mode === 'kg' ? 'Weight (kg)' : mode === 'tonne' ? 'Weight (tonne)' : 'Amount (Rs)'}
            </Label>
            <Input
              id="pad-value"
              autoFocus
              value={value}
              onChange={(e) => {
                setValue(e.target.value)
                setTouched(true)
              }}
              {...bigInputProps}
            />
          </div>
        )}

        {/* Preview. Always the same sentence, so the cashier reads one shape
            of number however the weight got here. */}
        <div className="rounded-lg border border-border bg-muted/40 px-4 py-3 text-sm">
          {line ? (
            <div className="space-y-1">
              {mode === 'gross_tare' && (
                <p className="text-muted-foreground tabular-nums">
                  {formatKg(line.gross_weight ?? 0)} gross − {formatKg(line.tare_weight ?? 0)} tare
                </p>
              )}
              {mode === 'amount' && Number.isFinite(typedAmount) && (
                <p className="text-muted-foreground tabular-nums">
                  Asked for {formatMoney(typedAmount)}
                  {round2(typedAmount) !== lineTotal && ' — priced to the nearest gram below'}
                </p>
              )}
              <p className="text-base font-semibold tabular-nums">
                {formatKg(line.quantity)} kg × {rate.toFixed(2)} = {formatMoney(lineTotal)}
              </p>
            </div>
          ) : (
            <p className={cn('text-muted-foreground', touched && 'text-destructive')}>{error ?? 'Enter a weight'}</p>
          )}
        </div>

        {keypad && (
          <NumericKeypad
            value={keypadValue}
            onChange={(next) => {
              setKeypadValue(next)
              setTouched(true)
            }}
            maxDecimals={mode === 'amount' ? 2 : 3}
            onEnter={confirm}
            enterLabel={editing ? 'Update' : 'Add'}
          />
        )}

        <DialogFooter className="gap-2 sm:justify-between">
          <div className="flex gap-2">
            <Button type="button" variant="ghost" onClick={() => setKeypad((k) => !k)}>
              {keypad ? 'Hide keypad' : 'Keypad'}
            </Button>
            {editing && onRemove && (
              <Button
                type="button"
                variant="ghost"
                className="text-destructive hover:text-destructive"
                onClick={() => {
                  onRemove()
                  onOpenChange(false)
                }}
              >
                Remove
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="button" onClick={confirm} disabled={!line}>
              {editing ? 'Update line' : 'Add to cart'}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
