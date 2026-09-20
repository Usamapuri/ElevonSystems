/**
 * Pure arithmetic behind the Receive Payment dialog: amount parsing (mirrors
 * `handlers/customers.go`'s `validRate(req.Amount) && req.Amount > 0` — more
 * than zero, at most 2 decimal places) and the balance-after preview shown
 * before the request goes out.
 *
 * The server is the authority: `POST /customers/:id/receipts` returns its
 * own `customer_balance_after` once the receipt actually posts, and that
 * figure — not this preview — is what the dialog and the statement show from
 * then on. This preview exists only so the person typing sees where the
 * account is headed before they commit.
 */

/** A receipt amount as a number, or null while the box holds nothing usable:
 * blank, non-numeric, negative, zero, finer than paisa, or absurdly large
 * (mirrors `products.go`'s `validRate`, which the receipt handler reuses). */
export function parseReceiptAmount(raw: string): number | null {
  const s = (raw ?? '').trim()
  if (s === '') return null
  if (!/^\d*(\.\d*)?$/.test(s) || s === '.') return null
  const dot = s.indexOf('.')
  if (dot >= 0 && s.length - dot - 1 > 2) return null
  const n = Number(s)
  if (!Number.isFinite(n) || n <= 0 || n >= 1e10) return null
  return Math.round(n * 100) / 100
}

/** `currentBalance − amount`, at paisa precision — the ledger's own
 * arithmetic (balance = Σdebit − Σcredit; a receipt posts a credit). Null
 * while the amount box is not yet usable, so the dialog shows nothing rather
 * than a stale or half-typed number. */
export function previewBalanceAfter(currentBalance: number, amount: number | null): number | null {
  if (amount === null || !Number.isFinite(currentBalance)) return null
  return Math.round((currentBalance - amount) * 100) / 100
}
