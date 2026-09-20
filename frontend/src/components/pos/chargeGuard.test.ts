import { describe, expect, it } from 'vitest'
import { dialogIsOpen, shouldOpenTender, type ChargeGuardState } from './chargeGuard'

function state(over: Partial<ChargeGuardState> = {}): ChargeGuardState {
  return { tenderOpen: false, pinOpen: false, pending: false, chargeDisabled: false, ...over }
}

describe('shouldOpenTender', () => {
  it('opens a new attempt when nothing is in the way', () => {
    expect(shouldOpenTender(state())).toBe(true)
  })

  it('refuses while the cart cannot be charged at all', () => {
    expect(shouldOpenTender(state({ chargeDisabled: true }))).toBe(false)
  })

  // The three that protect the idempotency key: re-opening the dialog mints
  // a fresh client_op_id, so a duplicate press during a live attempt would
  // re-key it and defeat the server's duplicate-POST protection on exactly
  // the lost-response path it exists for.
  it('refuses while the tender dialog is already open', () => {
    expect(shouldOpenTender(state({ tenderOpen: true }))).toBe(false)
  })

  it('refuses while the credit-limit PIN modal is up — that is a retry of the same attempt', () => {
    expect(shouldOpenTender(state({ pinOpen: true }))).toBe(false)
  })

  it('refuses while a POST /invoices is in flight', () => {
    expect(shouldOpenTender(state({ pending: true }))).toBe(false)
  })

  it('refuses when the dialog closed but the request has not settled', () => {
    // The tender dialog hides itself on some errors while the mutation is
    // still resolving; `pending` alone has to be enough to hold the key.
    expect(shouldOpenTender(state({ tenderOpen: false, pending: true }))).toBe(false)
  })
})

describe('dialogIsOpen', () => {
  it('is true while either dialog owns the screen', () => {
    expect(dialogIsOpen({ tenderOpen: false, pinOpen: false })).toBe(false)
    expect(dialogIsOpen({ tenderOpen: true, pinOpen: false })).toBe(true)
    expect(dialogIsOpen({ tenderOpen: false, pinOpen: true })).toBe(true)
  })
})
