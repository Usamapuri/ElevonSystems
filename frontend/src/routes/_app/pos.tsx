/**
 * The till (spec §6.2). Products left, cart right, recent sales underneath.
 *
 * Three things here are load-bearing and easy to break:
 *
 *  1. The preview is computed with exactly the inputs the server will use —
 *     quantities to 3 dp, each product's live rate, the invoice discount,
 *     taxRateFor(tender, settings), and buyer_registered = the customer is
 *     FBR "Registered" (a walk-in is unregistered, so further tax applies).
 *     It is still only a preview: POST /invoices reprices from products.rate
 *     and the response is what gets printed and what the ledger carries.
 *
 *  2. client_op_id is minted once per charge attempt and REUSED on the
 *     retry with an admin PIN after credit_limit_exceeded. That is what
 *     makes the retry safe: if the first POST actually committed and the
 *     409 was a race, the retry returns the existing invoice instead of
 *     ringing the sale twice.
 *
 *  3. Charge is off whenever the day gate blocks or the cart is empty. The
 *     server re-decides both; this only saves the cashier from ringing a
 *     whole sale into a 409.
 */
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import apiClient, { ApiClientError } from '@/api/client'
import { CartRail } from '@/components/pos/CartRail'
import { DayGateBanner } from '@/components/pos/DayGateBanner'
import { ProductTiles } from '@/components/pos/ProductTiles'
import { RECENT_INVOICES_KEY, RecentInvoices } from '@/components/pos/RecentInvoices'
import { TenderDialog, type TenderDetails } from '@/components/pos/TenderDialog'
import { WeightPad } from '@/components/pos/WeightPad'
import { PinEntryModal } from '@/components/shared/PinEntryModal'
import { useSettings } from '@/components/settings/useSettings'
import { toast } from '@/hooks/use-toast'
import { computeTotals, taxRateFor, type Totals } from '@/lib/pricing'
import { printInvoice } from '@/lib/print/printInvoice'
import { cartDiscount, cartReducer, emptyCart, pricingLines, requestLines, type CartLine } from '@/components/pos/cart'
import { evaluateDayGate, gateBlocks, gateFromErrorCode, type DayGate } from '@/components/pos/dayGate'
import { dialogIsOpen, shouldOpenTender } from '@/components/pos/chargeGuard'
import type { CreateInvoiceRequest, Customer, PaymentMethod, PrintDocument, Product } from '@/types'

export const Route = createFileRoute('/_app/pos')({ component: PosPage })

const DAY_CURRENT_KEY = ['day', 'current'] as const
const PRODUCTS_KEY = ['products', 'active'] as const

/** UUID v4 for client_op_id. crypto.randomUUID needs a secure context, which
 * a till served over plain http on a shop LAN is not, so fall back rather
 * than lose idempotency exactly where a flaky network makes it matter. */
function newClientOpId(): string {
  const c = globalThis.crypto
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()
  const bytes = new Uint8Array(16)
  if (c && typeof c.getRandomValues === 'function') c.getRandomValues(bytes)
  else for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

function PosPage() {
  const qc = useQueryClient()
  const [cart, dispatch] = useReducer(cartReducer, emptyCart)
  const [customer, setCustomer] = useState<Customer | null>(null)
  const [tender, setTender] = useState<PaymentMethod>('cash')
  const [search, setSearch] = useState('')

  const [padProduct, setPadProduct] = useState<Product | null>(null)
  const [padEditing, setPadEditing] = useState<CartLine | null>(null)
  const [padOpen, setPadOpen] = useState(false)

  const [tenderOpen, setTenderOpen] = useState(false)
  const [chargeError, setChargeError] = useState<string | null>(null)
  const [pinOpen, setPinOpen] = useState(false)
  const [pinError, setPinError] = useState<string | null>(null)
  const [gateOverride, setGateOverride] = useState<DayGate | null>(null)

  const searchRef = useRef<HTMLInputElement>(null)
  /** Minted on the first Charge press of an attempt; kept across the PIN
   * retry; cleared once the sale lands or the cashier walks away. */
  const clientOpId = useRef<string | null>(null)
  /** What the tender dialog last submitted, so the PIN retry can repeat it. */
  const lastDetails = useRef<TenderDetails | null>(null)

  const { data: settings } = useSettings()

  const products = useQuery({
    queryKey: PRODUCTS_KEY,
    queryFn: async () => {
      const res = await apiClient.getProducts({ active: true })
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load products')
      return res.data
    },
    staleTime: 5 * 60 * 1000,
  })

  const dayCurrent = useQuery({
    queryKey: DAY_CURRENT_KEY,
    queryFn: async () => {
      const res = await apiClient.getDayCurrent()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the business day')
      return res.data
    },
    refetchOnWindowFocus: true,
  })

  const gate: DayGate =
    gateOverride ??
    (dayCurrent.isLoading || !settings
      ? { kind: 'loading' }
      : evaluateDayGate(dayCurrent.data?.day ?? null, settings.day_boundary_hour, new Date()))

  // A successful refetch is the authority again: drop an override the
  // server's own 409 put there once the day query has been re-read.
  useEffect(() => {
    if (dayCurrent.isFetching) return
    setGateOverride(null)
    // dayCurrent.dataUpdatedAt changes on every settled refetch.
  }, [dayCurrent.dataUpdatedAt, dayCurrent.isFetching])

  const taxRate = settings ? taxRateFor(tender, settings) : 0
  const discount = cartDiscount(cart)
  const buyerRegistered = customer?.buyer_registration_type === 'Registered'

  const totals: Totals | null = useMemo(() => {
    if (!settings || cart.lines.length === 0 || !discount.valid) return null
    try {
      return computeTotals({
        lines: pricingLines(cart),
        discount_amount: discount.discount_amount,
        discount_percent: discount.discount_percent,
        tax_rate: taxRateFor(tender, settings),
        further_tax_rate: settings.further_tax_rate,
        buyer_registered: buyerRegistered,
      })
    } catch {
      // A rateless product or an unparseable discount: the rail falls back
      // to zeroes and Charge stays off rather than showing a wrong number.
      return null
    }
  }, [cart, settings, tender, buyerRegistered, discount.valid, discount.discount_amount, discount.discount_percent])

  const blocked = gateBlocks(gate)
  const chargeDisabled = blocked || cart.lines.length === 0 || !totals
  const chargeHint = blocked
    ? 'The business day has to be sorted out first.'
    : cart.lines.length === 0
      ? null
      : !discount.valid
        ? 'Check the discount.'
        : !totals
          ? 'One of these products has no usable rate.'
          : null

  const charge = useMutation({
    mutationFn: (pin?: string) => {
      const details = lastDetails.current
      if (!details) throw new Error('No tender details')
      if (!clientOpId.current) clientOpId.current = newClientOpId()
      const body: CreateInvoiceRequest = {
        client_op_id: clientOpId.current,
        lines: requestLines(cart),
        discount_amount: discount.discount_amount,
        discount_percent: discount.discount_percent,
        customer_id: customer?.id,
        payment_method: details.payment_method,
        payment_sub_method: details.payment_sub_method,
        payment_reference: details.payment_reference,
        notes: details.notes,
        pin,
      }
      return apiClient.createInvoice(body)
    },
    onSuccess: async (res) => {
      const invoice = res.data
      if (!invoice) {
        setChargeError(res.message || 'The sale did not come back')
        return
      }
      const document: PrintDocument = lastDetails.current?.document ?? settings?.receipt_default_document ?? 'thermal'
      clientOpId.current = null
      lastDetails.current = null
      setPinOpen(false)
      setPinError(null)
      setChargeError(null)
      setTenderOpen(false)
      dispatch({ type: 'clear' })
      setCustomer(null)
      setTender('cash')
      qc.invalidateQueries({ queryKey: RECENT_INVOICES_KEY })
      qc.invalidateQueries({ queryKey: DAY_CURRENT_KEY })
      if (invoice.customer_id) qc.invalidateQueries({ queryKey: ['customers'] })
      toast({ title: `Invoice ${invoice.invoice_number}`, variant: 'success' })
      searchRef.current?.focus()
      await printInvoice(invoice, document)
    },
    onError: (err: unknown) => {
      const code = err instanceof ApiClientError ? err.code : undefined
      const message = err instanceof Error ? err.message : 'Could not record the sale'

      const dayGate = gateFromErrorCode(code)
      if (dayGate) {
        setGateOverride(dayGate)
        setTenderOpen(false)
        setPinOpen(false)
        qc.invalidateQueries({ queryKey: DAY_CURRENT_KEY })
        return
      }
      if (code === 'credit_limit_exceeded') {
        // Same attempt, same client_op_id — the retry carries the PIN.
        setChargeError(message)
        setPinError(null)
        setPinOpen(true)
        return
      }
      if (code === 'invalid_pin') {
        setPinError(message)
        return
      }
      // Everything else — customer_required, credit_not_allowed,
      // product_not_found, invalid_quantity, … — is shown inline in the
      // tender dialog against the server's own wording, which is already
      // written for a cashier.
      setPinOpen(false)
      setChargeError(message)
    },
  })

  const openPadForProduct = useCallback((product: Product) => {
    setPadProduct(product)
    setPadEditing(null)
    setPadOpen(true)
  }, [])

  const openPadForLine = useCallback(
    (line: CartLine) => {
      const product = (products.data ?? []).find((p) => p.id === line.product_id)
      setPadProduct(
        product ?? {
          // The product was deactivated since the line was rung: keep editing
          // it against the rate the line already carries rather than wedging
          // the cart.
          id: line.product_id,
          name: line.product_name,
          sku: null,
          sell_by: 'weight',
          unit_label: 'kg',
          rate: line.rate,
          hs_code: null,
          fbr_uom: null,
          sort_order: 0,
          is_active: false,
          created_at: '',
          updated_at: '',
        },
      )
      setPadEditing(line)
      setPadOpen(true)
    },
    [products.data],
  )

  /**
   * Starts a NEW charge attempt — and only a new one. Opening the dialog
   * throws away the current client_op_id and the next POST mints a fresh
   * one, so a second press while the dialog is up, while the PIN modal is
   * up, or while the POST is in flight has to be a no-op: re-keying a live
   * attempt is what turns a lost response into a double-charge.
   */
  const openTender = () => {
    if (!shouldOpenTender({ tenderOpen, pinOpen, pending: charge.isPending, chargeDisabled })) return
    clientOpId.current = null
    setChargeError(null)
    setPinError(null)
    setTenderOpen(true)
  }

  // Hotkeys: / focuses the product search, F2 opens the tender dialog. Esc is
  // Radix's job (every dialog here closes on it), and both hotkeys stand
  // down entirely while a dialog owns the screen — F2 through a live
  // attempt is exactly the double-press that must not re-key it. openTender
  // is not memoized — it closes over chargeDisabled, which is cheap to
  // recompute every render — so it is listed here rather than wrapped in its
  // own useCallback just to satisfy this array.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (dialogIsOpen({ tenderOpen, pinOpen })) return
      const target = e.target as HTMLElement | null
      const typing =
        !!target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)
      if (e.key === '/' && !typing) {
        e.preventDefault()
        searchRef.current?.focus()
        return
      }
      if (e.key === 'F2') {
        e.preventDefault()
        openTender()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [tenderOpen, pinOpen, openTender])

  return (
    <div className="flex min-h-0 flex-col gap-4 p-4 md:p-6 lg:h-full">
      <DayGateBanner gate={gate} />

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-[1fr_360px]">
        <div className="flex min-h-0 flex-col gap-4">
          <ProductTiles
            products={products.data ?? []}
            isLoading={products.isLoading}
            error={products.error ? 'Could not load products' : null}
            search={search}
            onSearchChange={setSearch}
            onPick={openPadForProduct}
            searchRef={searchRef}
          />
          <RecentInvoices />
        </div>

        <CartRail
          cart={cart}
          dispatch={dispatch}
          totals={totals}
          discountValid={discount.valid}
          customer={customer}
          onCustomerChange={setCustomer}
          tender={tender}
          taxRate={taxRate}
          onEditLine={openPadForLine}
          onCharge={openTender}
          // Also off while a POST is in flight: the button is the other way
          // into openTender, and a second press must not re-key a live
          // attempt.
          chargeDisabled={chargeDisabled || charge.isPending}
          chargeHint={chargeHint}
        />
      </div>

      <WeightPad
        open={padOpen}
        onOpenChange={setPadOpen}
        product={padProduct}
        editing={padEditing}
        onConfirm={(line) => {
          if (padEditing) dispatch({ type: 'update', key: padEditing.key, line })
          else dispatch({ type: 'add', line })
        }}
        onRemove={padEditing ? () => dispatch({ type: 'remove', key: padEditing.key }) : undefined}
      />

      <TenderDialog
        open={tenderOpen}
        onOpenChange={(open) => {
          // TenderDialog already refuses to close while pending; the key is
          // only released once nothing is in flight and no PIN retry of
          // this attempt is still pending.
          if (!open && (charge.isPending || pinOpen)) return
          setTenderOpen(open)
          if (!open) {
            clientOpId.current = null
            setChargeError(null)
          }
        }}
        tender={tender}
        onTenderChange={setTender}
        totalPayable={totals?.total_payable ?? 0}
        customer={customer}
        defaultDocument={settings?.receipt_default_document ?? 'thermal'}
        pending={charge.isPending}
        error={chargeError}
        onCharge={(details) => {
          lastDetails.current = details
          setChargeError(null)
          charge.mutate(undefined)
        }}
      />

      <PinEntryModal
        open={pinOpen}
        onOpenChange={(open) => {
          // Dismissing this while the override POST is in flight would
          // strand the attempt with its key half-released.
          if (charge.isPending) return
          setPinOpen(open)
          if (!open) setPinError(null)
        }}
        title="Credit limit"
        description={chargeError ?? 'An admin PIN can let this sale through.'}
        submitLabel="Override and charge"
        pending={charge.isPending}
        error={pinError}
        onSubmit={(pin) => {
          setPinError(null)
          charge.mutate(pin)
        }}
      />
    </div>
  )
}
