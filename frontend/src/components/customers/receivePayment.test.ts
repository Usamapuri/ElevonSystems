import { describe, expect, it } from 'vitest'
import { parseReceiptAmount, previewBalanceAfter } from './receivePayment'

describe('parseReceiptAmount', () => {
  it('reads a plain amount', () => {
    expect(parseReceiptAmount('500')).toBe(500)
    expect(parseReceiptAmount(' 1250.50 ')).toBe(1250.5)
  })
  it('tolerates a half-typed decimal point', () => {
    expect(parseReceiptAmount('12.')).toBe(12)
  })
  it('is null while nothing usable has been typed', () => {
    expect(parseReceiptAmount('')).toBeNull()
    expect(parseReceiptAmount('   ')).toBeNull()
    expect(parseReceiptAmount('.')).toBeNull()
    expect(parseReceiptAmount('abc')).toBeNull()
  })
  it('refuses zero — a receipt must be more than zero', () => {
    expect(parseReceiptAmount('0')).toBeNull()
    expect(parseReceiptAmount('0.00')).toBeNull()
  })
  it('refuses negatives and sub-paisa', () => {
    expect(parseReceiptAmount('-5')).toBeNull()
    expect(parseReceiptAmount('12.345')).toBeNull()
  })
  it('refuses an absurdly large amount', () => {
    expect(parseReceiptAmount('10000000000')).toBeNull()
  })
  it('refuses more than 2 decimal places', () => {
    expect(parseReceiptAmount('19.999')).toBeNull()
  })
})

describe('previewBalanceAfter', () => {
  it('subtracts the amount from the current balance', () => {
    expect(previewBalanceAfter(5000, 2000)).toBe(3000)
  })
  it('can go negative — an overpayment leaves credit on the account', () => {
    expect(previewBalanceAfter(1000, 1500)).toBe(-500)
  })
  it('is null while the amount is not yet usable', () => {
    expect(previewBalanceAfter(5000, null)).toBeNull()
  })
  it('rounds to paisa precision', () => {
    expect(previewBalanceAfter(10.1, 0.05)).toBe(10.05)
  })
})
