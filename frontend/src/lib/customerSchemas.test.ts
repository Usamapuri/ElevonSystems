import { describe, expect, it } from 'vitest'
import { customerDialogSchema } from './customerSchemas'

const base = {
  name: 'Ali Traders',
  phone: '0300 1234567',
  ntn: '1234567-8',
  cnic: '3520112345678',
  buyer_registration_type: 'Registered' as const,
  address: 'Shop 4, Main Market',
  province: 'Punjab',
  credit_allowed: true,
  credit_limit: '5000',
  notes: 'VIP customer',
  is_active: true,
}

describe('customerDialogSchema', () => {
  it('accepts a fully populated customer', () => {
    expect(customerDialogSchema.safeParse(base).success).toBe(true)
  })

  it('accepts every optional field left blank', () => {
    const minimal = { ...base, phone: '', ntn: '', cnic: '', address: '', province: '', credit_limit: '', notes: '' }
    expect(customerDialogSchema.safeParse(minimal).success).toBe(true)
  })

  it('requires a name, at most 120 characters', () => {
    expect(customerDialogSchema.safeParse({ ...base, name: '' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, name: 'x'.repeat(121) }).success).toBe(false)
  })

  it('rejects a phone with letters or too many characters, allows digits/+/spaces', () => {
    expect(customerDialogSchema.safeParse({ ...base, phone: 'abc' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, phone: '+92 300 1234567' }).success).toBe(true)
    expect(customerDialogSchema.safeParse({ ...base, phone: '0'.repeat(31) }).success).toBe(false)
  })

  it('rejects an NTN or CNIC with the wrong characters', () => {
    expect(customerDialogSchema.safeParse({ ...base, ntn: 'NTN-123' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, cnic: '3520-1123456-7' }).success).toBe(false)
  })

  it('rejects a buyer_registration_type outside the enum', () => {
    expect(customerDialogSchema.safeParse({ ...base, buyer_registration_type: 'Foreign' }).success).toBe(false)
  })

  it('rejects a province over 40 characters', () => {
    expect(customerDialogSchema.safeParse({ ...base, province: 'x'.repeat(41) }).success).toBe(false)
  })

  it('rejects a negative or over-precise credit limit, accepts zero', () => {
    expect(customerDialogSchema.safeParse({ ...base, credit_limit: '-1' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, credit_limit: '10.999' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, credit_limit: '0' }).success).toBe(true)
  })
})
