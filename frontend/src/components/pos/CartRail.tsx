/**
 * The right rail: what is being sold, to whom, and what it comes to.
 *
 * The totals are lib/pricing.computeTotals over the same inputs the server
 * will use — quantities to 3 dp, each product's rate, the invoice discount,
 * the tax rate for the tender the cashier has chosen, and whether the buyer
 * is FBR-registered. They are a preview: the charge is recomputed
 * server-side from products.rate and that figure is what the customer pays.
 */
import { Pencil, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Money } from '@/components/shared/Money'
import { CustomerPicker } from '@/components/pos/CustomerPicker'
import { formatMoney } from '@/lib/money'
import { cn } from '@/lib/utils'
import { formatKg } from '@/lib/weight'
import type { Totals } from '@/lib/pricing'
import type { Customer, PaymentMethod } from '@/types'
import type { CartAction, CartLine, CartState } from './cart'

interface Props {
  cart: CartState
  dispatch: (action: CartAction) => void
  /** null while the cart is empty or the discount does not parse. */
  totals: Totals | null
  discountValid: boolean
  customer: Customer | null
  onCustomerChange: (customer: Customer | null) => void
  tender: PaymentMethod
  /** taxRateFor(tender, settings) — shown even with an empty cart, so the
   * rail always says which rate this tender attracts. */
  taxRate: number
  onEditLine: (line: CartLine) => void
  onCharge: () => void
  chargeDisabled: boolean
  /** Shown under the Charge button when it is off. */
  chargeHint: string | null
}

const TENDER_LABEL: Record<PaymentMethod, string> = {
  cash: 'Cash',
  card: 'Card',
  online: 'Online',
  credit: 'Credit',
}

export function CartRail({
  cart,
  dispatch,
  totals,
  discountValid,
  customer,
  onCustomerChange,
  tender,
  taxRate,
  onEditLine,
  onCharge,
  chargeDisabled,
  chargeHint,
}: Props) {
  const empty = cart.lines.length === 0

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 rounded-xl border border-border bg-card p-4">
      <div className="flex items-baseline justify-between">
        <h2 className="text-lg font-semibold">Cart</h2>
        {!empty && (
          <button
            type="button"
            className="text-xs text-muted-foreground hover:text-destructive"
            onClick={() => dispatch({ type: 'clear' })}
          >
            Clear all
          </button>
        )}
      </div>

      <div className="min-h-[80px] flex-1 overflow-y-auto pr-1">
        {empty ? (
          <p className="pt-6 text-center text-sm text-muted-foreground">
            Tap a product to weigh it.
          </p>
        ) : (
          <ul className="space-y-2">
            {cart.lines.map((line, i) => {
              const lineTotal = totals?.lines[i]
              return (
                <li key={line.key} className="rounded-lg border border-border px-3 py-2">
                  <div className="flex items-start justify-between gap-2">
                    <button
                      type="button"
                      className="min-w-0 flex-1 text-left"
                      onClick={() => onEditLine(line)}
                    >
                      <p className="truncate font-medium">{line.product_name}</p>
                      <p className="text-xs text-muted-foreground tabular-nums">
                        {formatKg(line.quantity)} kg × {line.rate.toFixed(2)}
                        {line.entered_as === 'gross_tare' && line.gross_weight !== undefined && line.tare_weight !== undefined && (
                          <> · {formatKg(line.gross_weight)} − {formatKg(line.tare_weight)}</>
                        )}
                        {line.entered_as === 'tonne' && <> · {(line.quantity / 1000).toFixed(3)} t</>}
                        {line.entered_as === 'amount' && line.typed_amount !== undefined && (
                          <> · asked {formatMoney(line.typed_amount)}</>
                        )}
                      </p>
                    </button>
                    <div className="flex shrink-0 items-center gap-1">
                      <span className="text-sm font-medium tabular-nums">
                        <Money amount={lineTotal ? lineTotal.line_total : line.quantity * line.rate} />
                      </span>
                      <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="Edit line" onClick={() => onEditLine(line)}>
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 text-muted-foreground hover:text-destructive"
                        aria-label="Remove line"
                        onClick={() => dispatch({ type: 'remove', key: line.key })}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </div>

      {/* Discount */}
      <div className="space-y-1.5">
        <span className="text-sm font-medium">Discount</span>
        <div className="flex gap-2">
          <div className="flex rounded-md bg-muted p-1">
            {(['amount', 'percent'] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => dispatch({ type: 'set_discount_mode', mode: m })}
                className={cn(
                  'rounded px-3 text-sm font-medium transition-colors',
                  cart.discountMode === m ? 'bg-background shadow-sm' : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {m === 'amount' ? 'Rs' : '%'}
              </button>
            ))}
          </div>
          <Input
            inputMode="decimal"
            placeholder="0"
            className={cn('tabular-nums', !discountValid && 'border-destructive')}
            value={cart.discountValue}
            onChange={(e) => dispatch({ type: 'set_discount_value', value: e.target.value })}
          />
        </div>
        {!discountValid && (
          <p className="text-xs text-destructive">
            {cart.discountMode === 'percent' ? 'Enter a percentage between 0 and 100' : 'Enter a rupee amount of zero or more'}
          </p>
        )}
      </div>

      <CustomerPicker value={customer} onChange={onCustomerChange} required={tender === 'credit'} />

      {/* Totals */}
      <dl className="space-y-1 border-t border-border pt-3 text-sm">
        <Row label="Subtotal" value={totals?.subtotal ?? 0} />
        {(totals?.discount_amount ?? 0) > 0 && (
          <Row label="Discount" value={-(totals?.discount_amount ?? 0)} className="text-muted-foreground" />
        )}
        <Row label={`Tax (${formatRate(taxRate)} · ${TENDER_LABEL[tender]})`} value={totals?.tax_amount ?? 0} />
        {(totals?.further_tax_amount ?? 0) > 0 && (
          <Row label="Further tax (unregistered buyer)" value={totals?.further_tax_amount ?? 0} />
        )}
        {totals && totals.rounding_adjustment !== 0 && (
          <Row label="Rounding" value={totals.rounding_adjustment} className="text-muted-foreground" />
        )}
        <div className="flex items-baseline justify-between border-t border-border pt-2">
          <dt className="text-base font-semibold">Total payable</dt>
          <dd className="text-2xl font-bold tabular-nums">{formatMoney(totals?.total_payable ?? 0)}</dd>
        </div>
      </dl>

      <Button className="h-14 text-lg" disabled={chargeDisabled} onClick={onCharge}>
        Charge {totals ? formatMoney(totals.total_payable) : ''}
      </Button>
      {chargeHint && <p className="text-center text-xs text-muted-foreground">{chargeHint}</p>}
    </div>
  )
}

/** 0.18 → "18%", 0.185 → "18.5%". Settings store fractions. */
function formatRate(rate: number): string {
  const pct = rate * 100
  return `${Number(pct.toFixed(4))}%`
}

function Row({ label, value, className }: { label: string; value: number; className?: string }) {
  return (
    <div className={cn('flex items-baseline justify-between', className)}>
      <dt>{label}</dt>
      <dd className="tabular-nums">
        <Money amount={value} />
      </dd>
    </div>
  )
}
