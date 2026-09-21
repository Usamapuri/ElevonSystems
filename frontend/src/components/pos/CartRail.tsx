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
  /** Grid placement from the page; the rail owns nothing about the layout. */
  className?: string
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
  className,
}: Props) {
  const empty = cart.lines.length === 0

  return (
    <div className={cn('flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-card shadow-lg', className)}>
      <div className="flex items-center justify-between px-4 pb-2 pt-4">
        <h2 className="text-base font-bold">Cart</h2>
        {!empty && (
          <button
            type="button"
            className="rounded-md px-2 py-1 text-xs font-semibold text-muted-foreground transition-colors hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => dispatch({ type: 'clear' })}
          >
            Clear all
          </button>
        )}
      </div>

      <div className="min-h-[80px] flex-1 overflow-y-auto px-4">
        {empty ? (
          <p className="pt-8 text-center text-sm text-muted-foreground">
            Nothing on the scale yet. Tap a product to weigh it.
          </p>
        ) : (
          <ul className="space-y-2">
            {cart.lines.map((line, i) => {
              const lineTotal = totals?.lines[i]
              return (
                <li key={line.key} className="rounded-md border border-border px-3 py-2">
                  <div className="flex items-start justify-between gap-2">
                    <button
                      type="button"
                      className="min-w-0 flex-1 py-1 text-left"
                      onClick={() => onEditLine(line)}
                    >
                      <p className="truncate text-sm font-semibold">{line.product_name}</p>
                      <p className="tabular text-xs text-muted-foreground">
                        {formatKg(line.quantity)} kg at {line.rate.toFixed(2)}
                        {line.entered_as === 'gross_tare' && line.gross_weight !== undefined && line.tare_weight !== undefined && (
                          <> — {formatKg(line.tare_weight)} before, {formatKg(line.gross_weight)} after</>
                        )}
                        {line.entered_as === 'tonne' && <> — {(line.quantity / 1000).toFixed(3)} t</>}
                        {line.entered_as === 'amount' && line.typed_amount !== undefined && (
                          <> — asked {formatMoney(line.typed_amount)}</>
                        )}
                      </p>
                    </button>
                    <div className="flex shrink-0 items-center gap-0.5">
                      <span className="tabular text-sm font-bold">
                        <Money amount={lineTotal ? lineTotal.line_total : line.quantity * line.rate} />
                      </span>
                      <Button variant="ghost" size="icon" className="h-11 w-11" aria-label="Edit line" onClick={() => onEditLine(line)}>
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-11 w-11 text-muted-foreground hover:text-destructive"
                        aria-label="Remove line"
                        onClick={() => dispatch({ type: 'remove', key: line.key })}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </div>

      <div className="space-y-3 px-4 pt-3">
        {/* Discount */}
        <div className="space-y-1.5">
          <span className="text-xs font-semibold text-muted-foreground">Discount</span>
          <div className="flex gap-2">
            <div className="flex shrink-0 rounded-md border border-border bg-secondary p-1">
              {(['amount', 'percent'] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => dispatch({ type: 'set_discount_mode', mode: m })}
                  className={cn(
                    'w-10 rounded-sm text-sm font-bold transition-colors',
                    cart.discountMode === m ? 'bg-card text-foreground' : 'text-muted-foreground hover:text-foreground',
                  )}
                >
                  {m === 'amount' ? 'Rs' : '%'}
                </button>
              ))}
            </div>
            <Input
              inputMode="decimal"
              placeholder="0"
              aria-label={cart.discountMode === 'percent' ? 'Discount percentage' : 'Discount amount in rupees'}
              className={cn('tabular', !discountValid && 'border-destructive')}
              value={cart.discountValue}
              onChange={(e) => dispatch({ type: 'set_discount_value', value: e.target.value })}
            />
          </div>
          {!discountValid && (
            <p className="text-xs font-semibold text-destructive">
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
          <Row label={`Tax ${formatRate(taxRate)} on ${TENDER_LABEL[tender].toLowerCase()}`} value={totals?.tax_amount ?? 0} />
          {(totals?.further_tax_amount ?? 0) > 0 && (
            <Row label="Further tax (unregistered buyer)" value={totals?.further_tax_amount ?? 0} />
          )}
          {totals && totals.rounding_adjustment !== 0 && (
            <Row label="Rounding" value={totals.rounding_adjustment} className="text-muted-foreground" />
          )}
        </dl>
      </div>

      {/*
        The payable plate. This is the one loud element in the whole app and
        it is loud on purpose: a weighbridge reads out in big lit digits
        against a dark housing, and the figure the customer is about to hand
        over money for deserves the same treatment. Everything above it is
        deliberately quiet so that this is the thing the eye lands on.
      */}
      <div className="mt-3 bg-rail px-4 pb-4 pt-3">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-xs font-bold text-rail-muted">Total payable</span>
          <span className="tabular truncate text-[2.5rem] font-extrabold leading-none text-primary">
            {formatMoney(totals?.total_payable ?? 0)}
          </span>
        </div>
        <Button
          className="mt-3 h-14 w-full text-base focus-visible:ring-primary focus-visible:ring-offset-rail disabled:bg-white/10 disabled:text-rail-muted"
          disabled={chargeDisabled}
          onClick={onCharge}
        >
          Charge {totals ? formatMoney(totals.total_payable) : ''}
        </Button>
        {chargeHint && <p className="mt-2 text-center text-xs font-medium text-rail-muted">{chargeHint}</p>}
      </div>
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
    <div className={cn('flex items-baseline justify-between gap-3', className)}>
      <dt className="truncate">{label}</dt>
      <dd className="tabular shrink-0 font-semibold">
        <Money amount={value} />
      </dd>
    </div>
  )
}
