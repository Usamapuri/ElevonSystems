/**
 * The weight pad's arithmetic (spec §6.2). An LPG till rings a sale four
 * ways — kg, tonne, a rupee amount the customer asked for, or gross minus
 * the cylinder's tare — and every one of them resolves to the same thing on
 * the wire: a net weight in kg to three decimal places.
 *
 * This file owns that resolution and nothing else. It never prices anything:
 * money comes from lib/pricing.ts for the preview and from the server for
 * the figure that is actually charged.
 *
 * The 3 dp contract is the server's too (handlers/invoices.go validWeight
 * accepts a positive weight with at most 3 dp, and for gross_tare lines it
 * re-checks that gross − tare equals the quantity within half a gram). Both
 * are satisfied here by rounding gross and tare to 3 dp BEFORE subtracting,
 * so the difference the server recomputes is the difference we sent.
 */

/** The four pad modes. These strings are the `entered_as` column values
 * (invoice_lines_entered_as_check) — do not rename them casually. */
export type WeightMode = 'kg' | 'tonne' | 'amount' | 'gross_tare'

export const WEIGHT_MODES: WeightMode[] = ['kg', 'tonne', 'amount', 'gross_tare']

/** Mirrors the server's ceiling (validWeight rejects v >= 1e9, the bound of
 * NUMERIC(14,3) with room to spare). */
export const MAX_KG = 1e9

/** What the pad holds while the cashier types: `value` is the single number
 * for kg / tonne / amount, `gross` and `tare` are the two fields of the
 * gross − tare mode. Raw strings, because an in-progress "12." is a real
 * state of the input that Number() would silently turn into 12. */
export interface WeightPadValues {
  value?: string
  gross?: string
  tare?: string
}

/** Exactly the per-line fields POST /invoices accepts besides product_id.
 * gross_weight/tare_weight are present only for gross_tare lines — the
 * server refuses to store two weights nobody weighed. */
export interface LineInput {
  quantity: number
  entered_as: WeightMode
  gross_weight?: number
  tare_weight?: number
}

/** A human message for the pad to show under the input. Never a code: this
 * is client-side input validation, not a server error. */
export interface LineInputError {
  error: string
}

export type LineInputResult = LineInput | LineInputError

export function isLineInputError(r: LineInputResult): r is LineInputError {
  return 'error' in r
}

/**
 * Rounds to 3 dp, half up, without letting float multiplication decide a
 * tie. Scaling straight to the target grid (Math.round(n * 1000)) misrounds
 * genuine ties whose float product lands one ULP low — the same failure
 * lib/pricing.ts documents at length for round2. Scaling to micro-units
 * first puts that ~1e-16 relative error nowhere near the 0.5 threshold, and
 * the tie is then broken by integer arithmetic.
 */
export function round3(n: number): number {
  if (!Number.isFinite(n)) return NaN
  const micro = Math.round(n * 1e6)
  const neg = micro < 0
  const milli = Math.floor((Math.abs(micro) + 500) / 1000)
  return (neg ? -milli : milli) / 1000
}

/** 1.5 t → 1500 kg. */
export function kgFromTonne(tonnes: number): number {
  return round3(tonnes * 1000)
}

/**
 * "Give me Rs 3,000 of gas" → the weight that buys it at today's rate.
 * The rupee figure the customer named is NOT the invoice total: the server
 * reprices round3(amount ÷ rate) × rate and the result can differ by a
 * paisa or two, which is why the pad shows both numbers.
 * Returns NaN when rate is not usable; callers check (toLineInput does).
 */
export function kgFromAmount(amount: number, rate: number): number {
  if (!Number.isFinite(rate) || rate <= 0) return NaN
  return round3(amount / rate)
}

/** Net weight off the scale. Both arguments should already be 3 dp (see the
 * file comment) — toLineInput rounds them before calling this. */
export function netFromGrossTare(gross: number, tare: number): number {
  return round3(gross - tare)
}

/**
 * Bare 3-decimal weight for the pad and the cart line ("12.500"). This is
 * deliberately NOT lib/money.ts's formatKg, which adds thousands separators
 * and a " kg" suffix for display; this one is the raw number an input field
 * and a "12.500 kg × 265.00" line want.
 */
export function formatKg(n: number): string {
  if (!Number.isFinite(n)) return '0.000'
  return round3(n).toFixed(3)
}

/** Parses a typed field. Returns null for blank, non-numeric or infinite —
 * every one of which is "the cashier has not finished typing", not a value. */
function parseTyped(raw: string | undefined): number | null {
  const s = (raw ?? '').trim()
  if (s === '') return null
  const n = Number(s)
  if (!Number.isFinite(n)) return null
  return n
}

/** Shared tail checks: a quantity has to be a real, positive, in-range 3 dp
 * weight whatever mode produced it. */
function finish(quantity: number, enteredAs: WeightMode, zeroMessage: string): LineInputResult {
  if (!Number.isFinite(quantity)) return { error: 'Enter a number' }
  if (quantity <= 0) return { error: zeroMessage }
  if (quantity >= MAX_KG) return { error: 'That weight is too large' }
  return { quantity, entered_as: enteredAs }
}

/**
 * Turns what the pad holds into the line the till will post, or into the
 * message to show the cashier. `rate` is the product's rate per kg and is
 * only consulted by amount mode.
 */
export function toLineInput(mode: WeightMode, values: WeightPadValues, rate: number): LineInputResult {
  switch (mode) {
    case 'kg': {
      const kg = parseTyped(values.value)
      if (kg === null) return { error: 'Enter a weight in kg' }
      return finish(round3(kg), 'kg', 'Weight must be more than zero')
    }
    case 'tonne': {
      const tonnes = parseTyped(values.value)
      if (tonnes === null) return { error: 'Enter a weight in tonnes' }
      return finish(kgFromTonne(tonnes), 'tonne', 'Weight must be more than zero')
    }
    case 'amount': {
      const amount = parseTyped(values.value)
      if (amount === null) return { error: 'Enter a rupee amount' }
      if (!Number.isFinite(rate) || rate <= 0) {
        return { error: 'This product has no rate — set it on the Rates screen first' }
      }
      return finish(kgFromAmount(amount, rate), 'amount', 'Amount must be more than zero')
    }
    case 'gross_tare': {
      const grossRaw = parseTyped(values.gross)
      if (grossRaw === null) return { error: 'Enter the gross weight' }
      const tareRaw = parseTyped(values.tare)
      if (tareRaw === null) return { error: 'Enter the tare weight' }
      if (tareRaw < 0) return { error: 'Tare cannot be negative' }
      // Round both ends first so gross − tare on the server (which re-checks
      // to within half a gram) reproduces exactly the quantity we send.
      const gross = round3(grossRaw)
      const tare = round3(tareRaw)
      if (gross <= tare) return { error: 'Gross must be more than tare' }
      const net = netFromGrossTare(gross, tare)
      const result = finish(net, 'gross_tare', 'Gross must be more than tare')
      if (isLineInputError(result)) return result
      return { ...result, gross_weight: gross, tare_weight: tare }
    }
  }
}

/** The pad's mode toggle labels. */
export function modeLabel(mode: WeightMode): string {
  switch (mode) {
    case 'kg':
      return 'kg'
    case 'tonne':
      return 'tonne'
    case 'amount':
      return 'amount'
    case 'gross_tare':
      return 'gross − tare'
  }
}
