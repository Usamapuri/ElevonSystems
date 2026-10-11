/**
 * Deal rates (spec 2026-10-12 §3): a cashier may name a rate per cart line.
 * Inside the owner's band (settings.rate_override_band, Rs/kg) it stands on
 * its own; outside it the server demands an admin PIN. These mirror the
 * server's checks so the till can say "needs a PIN" before the POST — the
 * server's answer is still the authority.
 */

const paisa = (v: number) => Math.round(v * 100)

/** "249.5" → 249.5. Blank, zero, negative, more than 2 dp or non-numeric → null. */
export function parseRateInput(text: string): number | null {
  const t = text.trim()
  if (!/^\d+(\.\d{1,2})?$/.test(t)) return null
  const v = Number(t)
  return v > 0 ? v : null
}

/** Whole-paisa compare, like the server: the band is inclusive. */
export function needsPin(listRate: number, dealRate: number, band: number): boolean {
  return Math.abs(paisa(dealRate) - paisa(listRate)) > paisa(band)
}

/** The rate a line is priced at: the deal rate when one is set, else the list rate. */
export function effectiveRate(line: { rate: number; rate_override?: number }): number {
  return line.rate_override ?? line.rate
}
