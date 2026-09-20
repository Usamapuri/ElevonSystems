import { describe, expect, it } from 'vitest'
import {
  businessSchema, creditSchema, dayCloseSchema, fractionToPercent, linesToText, percentToFraction,
  receiptSchema, receiptToPatch, taxSchema, taxToPatch, textToLines,
} from './settings'

describe('percent ↔ fraction', () => {
  it('formats fractions as clean percentages', () => {
    expect(fractionToPercent(0)).toBe('0')
    expect(fractionToPercent(0.18)).toBe('18')
    expect(fractionToPercent(0.185)).toBe('18.5')
    expect(fractionToPercent(null)).toBe('')
  })
  it('parses percentages to 6-dp fractions without float noise', () => {
    expect(percentToFraction('18')).toBe(0.18)
    expect(percentToFraction('18.5')).toBe(0.185)
    expect(percentToFraction('4')).toBe(0.04)
    expect(percentToFraction('0')).toBe(0)
  })
  it('round-trips every seeded rate shape', () => {
    for (const f of [0, 0.04, 0.17, 0.18, 0.185, 1]) expect(percentToFraction(fractionToPercent(f))).toBe(f)
  })
})

describe('taxSchema', () => {
  const base = { tax_rate_cash: '18', tax_rate_card: '18', tax_rate_online: '18', tax_rate_credit: '', further_tax_rate: '0', default_hs_code: '' }
  it('accepts blanks for credit (same as cash) and HS code', () => {
    expect(taxSchema.safeParse(base).success).toBe(true)
  })
  it('rejects over 100, non-numeric and malformed HS codes', () => {
    expect(taxSchema.safeParse({ ...base, tax_rate_cash: '101' }).success).toBe(false)
    expect(taxSchema.safeParse({ ...base, tax_rate_cash: 'abc' }).success).toBe(false)
    expect(taxSchema.safeParse({ ...base, default_hs_code: '27111910' }).success).toBe(false)
    expect(taxSchema.safeParse({ ...base, default_hs_code: '2711.1910' }).success).toBe(true)
  })
  it('converts to a fraction patch with null credit rate', () => {
    expect(taxToPatch({ ...base, tax_rate_credit: '' })).toEqual({
      tax_rate_cash: 0.18, tax_rate_card: 0.18, tax_rate_online: 0.18, tax_rate_credit: null, further_tax_rate: 0, default_hs_code: '',
    })
    expect(taxToPatch({ ...base, tax_rate_credit: '20' }).tax_rate_credit).toBe(0.2)
  })
})

describe('receipt lines', () => {
  it('splits on newlines, trims, drops blanks', () => {
    expect(textToLines(' NTN 123 \n\n  Thank you  \n')).toEqual(['NTN 123', 'Thank you'])
    expect(linesToText(['a', 'b'])).toBe('a\nb')
  })
  it('caps at six lines of 64 characters and keeps printable within paper', () => {
    const base = { receipt_paper_width_mm: 80 as const, receipt_printable_area_mm: 72, receipt_logo_url: '', receipt_header_lines: '', receipt_footer_lines: '', receipt_default_document: 'thermal' as const }
    expect(receiptSchema.safeParse(base).success).toBe(true)
    expect(receiptSchema.safeParse({ ...base, receipt_header_lines: 'a\nb\nc\nd\ne\nf\ng' }).success).toBe(false)
    expect(receiptSchema.safeParse({ ...base, receipt_footer_lines: 'x'.repeat(65) }).success).toBe(false)
    expect(receiptSchema.safeParse({ ...base, receipt_paper_width_mm: 58 }).success).toBe(false)
    expect(receiptToPatch({ ...base, receipt_header_lines: 'a\nb' }).receipt_header_lines).toEqual(['a', 'b'])
  })
})

describe('other sections', () => {
  it('business needs a name and a 0–12 boundary hour', () => {
    const ok = { business_name: 'Gas House', business_address: '', business_phone: '', business_ntn: '', business_strn: '', business_province: '', day_boundary_hour: 0 }
    expect(businessSchema.safeParse(ok).success).toBe(true)
    expect(businessSchema.safeParse({ ...ok, business_name: ' ' }).success).toBe(false)
    expect(businessSchema.safeParse({ ...ok, day_boundary_hour: 13 }).success).toBe(false)
  })
  it('day close threshold is non-negative; credit flag is boolean', () => {
    expect(dayCloseSchema.safeParse({ day_close_variance_threshold: -1 }).success).toBe(false)
    expect(dayCloseSchema.safeParse({ day_close_variance_threshold: 100 }).success).toBe(true)
    expect(creditSchema.safeParse({ credit_limit_enforced: true }).success).toBe(true)
  })
})
