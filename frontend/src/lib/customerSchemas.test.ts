import { describe, expect, it } from 'vitest'
import { FBR_PROVINCES, canonicalProvince, customerDialogSchema } from './customerSchemas'

const base = {
  name: 'Ali Traders',
  phone: '0300 1234567',
  ntn: '1234567-8',
  cnic: '3520112345678',
  buyer_registration_type: 'Registered' as const,
  address: 'Shop 4, Main Market',
  province: 'PUNJAB',
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
    expect(customerDialogSchema.safeParse({ ...base, ntn: 'AB1234' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, cnic: '3520-1123456-7' }).success).toBe(false)
  })

  // FBR issues some NTNs with a leading letter (A723262 on a 2023 AOP
  // certificate; the sandbox validates it as a registered buyer).
  it('accepts an NTN with one leading letter, as FBR issues them', () => {
    expect(customerDialogSchema.safeParse({ ...base, ntn: 'A723262' }).success).toBe(true)
    expect(customerDialogSchema.safeParse({ ...base, ntn: 'a723262-4' }).success).toBe(true)
  })

  it('rejects a buyer_registration_type outside the enum', () => {
    expect(customerDialogSchema.safeParse({ ...base, buyer_registration_type: 'Foreign' }).success).toBe(false)
  })

  // The gateway refuses any buyer province outside its seven names (0074), so
  // the form offers exactly those and nothing typed.
  it('accepts only FBR province names or blank', () => {
    expect(FBR_PROVINCES).toHaveLength(7)
    for (const p of FBR_PROVINCES) expect(customerDialogSchema.safeParse({ ...base, province: p }).success).toBe(true)
    expect(customerDialogSchema.safeParse({ ...base, province: '' }).success).toBe(true)
    expect(customerDialogSchema.safeParse({ ...base, province: 'ICT' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, province: 'Punjab' }).success).toBe(false)
  })

  it('canonicalProvince maps what older rows stored onto the FBR spelling, else blank', () => {
    expect(canonicalProvince('Punjab')).toBe('PUNJAB')
    expect(canonicalProvince(' capital territory ')).toBe('CAPITAL TERRITORY')
    expect(canonicalProvince('ICT')).toBe('')
    expect(canonicalProvince(null)).toBe('')
  })

  it('rejects a negative or over-precise credit limit, accepts zero', () => {
    expect(customerDialogSchema.safeParse({ ...base, credit_limit: '-1' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, credit_limit: '10.999' }).success).toBe(false)
    expect(customerDialogSchema.safeParse({ ...base, credit_limit: '0' }).success).toBe(true)
  })
})
