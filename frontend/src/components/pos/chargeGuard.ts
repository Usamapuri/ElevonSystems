/**
 * When opening the tender dialog is allowed to start a NEW charge attempt.
 *
 * This exists because opening the dialog is the moment the till throws away
 * the current `client_op_id` and mints a fresh one. That is right for a new
 * sale and wrong for everything else: a second F2 (or a second Charge press)
 * while the dialog is already up, while the credit-limit PIN modal is up, or
 * while the POST is still in flight would re-key an attempt that is already
 * running — and the whole point of the key is that a retry after a lost
 * response returns the invoice the server already committed instead of
 * ringing the sale a second time.
 *
 * So the rule is: only a genuinely new attempt resets the key, and nothing
 * that could be a duplicate press gets through.
 */
export interface ChargeGuardState {
  /** The tender dialog is already open. */
  tenderOpen: boolean
  /** The credit-limit PIN modal is up — a retry of the current attempt. */
  pinOpen: boolean
  /** A POST /invoices is in flight. */
  pending: boolean
  /** No open day, empty cart, bad discount, unusable rate. */
  chargeDisabled: boolean
}

export function shouldOpenTender({ tenderOpen, pinOpen, pending, chargeDisabled }: ChargeGuardState): boolean {
  if (chargeDisabled) return false
  if (tenderOpen || pinOpen || pending) return false
  return true
}

/** True when a dialog owns the screen, so the global hotkeys should keep
 * their hands off it (Esc is each dialog's own job). */
export function dialogIsOpen({ tenderOpen, pinOpen }: Pick<ChargeGuardState, 'tenderOpen' | 'pinOpen'>): boolean {
  return tenderOpen || pinOpen
}
