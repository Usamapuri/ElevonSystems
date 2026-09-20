import { z } from 'zod'
import type { AppSettings, SettingsPatch } from '@/types'

// ── percent ↔ fraction (the API stores fractions; people type percentages) ──

/** 0.185 → '18.5'; null → '' (credit rate "same as cash"). */
export function fractionToPercent(fraction: number | null): string {
  if (fraction === null) return ''
  return String(Number((fraction * 100).toFixed(4)))
}

/** '18.5' → 0.185 (6 dp, no float noise). Callers validate first. */
export function percentToFraction(percent: string): number {
  return Math.round(Number(percent) * 10_000) / 1_000_000
}

const percent = z
  .string()
  .trim()
  .regex(/^\d{1,3}(\.\d{1,4})?$/, 'Enter a percentage such as 18 or 18.5')
  .refine((v) => Number(v) <= 100, 'Cannot exceed 100%')

const optionalPercent = z.union([z.literal(''), percent])

// ── Business ─────────────────────────────────────────────────────────────

export const businessSchema = z.object({
  business_name: z.string().trim().min(1, 'Business name is required').max(120),
  business_address: z.string().trim().max(300),
  business_phone: z.string().trim().max(30),
  business_ntn: z.string().trim().max(20),
  business_strn: z.string().trim().max(30),
  business_province: z.string().trim().max(40),
  day_boundary_hour: z.number({ invalid_type_error: 'Enter an hour' }).int().min(0).max(12),
})
export type BusinessFormValues = z.infer<typeof businessSchema>

export function businessFromSettings(s: AppSettings): BusinessFormValues {
  return {
    business_name: s.business_name,
    business_address: s.business_address,
    business_phone: s.business_phone,
    business_ntn: s.business_ntn,
    business_strn: s.business_strn,
    business_province: s.business_province,
    day_boundary_hour: s.day_boundary_hour,
  }
}

// ── Tax ──────────────────────────────────────────────────────────────────

export const taxSchema = z.object({
  tax_rate_cash: percent,
  tax_rate_card: percent,
  tax_rate_online: percent,
  /** Blank = use the cash rate. */
  tax_rate_credit: optionalPercent,
  further_tax_rate: percent,
  default_hs_code: z.union([z.literal(''), z.string().trim().regex(/^\d{4}\.\d{4}$/, 'Format is 2711.1910')]),
})
export type TaxFormValues = z.infer<typeof taxSchema>

export function taxFromSettings(s: AppSettings): TaxFormValues {
  return {
    tax_rate_cash: fractionToPercent(s.tax_rate_cash),
    tax_rate_card: fractionToPercent(s.tax_rate_card),
    tax_rate_online: fractionToPercent(s.tax_rate_online),
    tax_rate_credit: fractionToPercent(s.tax_rate_credit),
    further_tax_rate: fractionToPercent(s.further_tax_rate),
    default_hs_code: s.default_hs_code,
  }
}

export function taxToPatch(v: TaxFormValues): SettingsPatch {
  return {
    tax_rate_cash: percentToFraction(v.tax_rate_cash),
    tax_rate_card: percentToFraction(v.tax_rate_card),
    tax_rate_online: percentToFraction(v.tax_rate_online),
    tax_rate_credit: v.tax_rate_credit === '' ? null : percentToFraction(v.tax_rate_credit),
    further_tax_rate: percentToFraction(v.further_tax_rate),
    default_hs_code: v.default_hs_code,
  }
}

// ── Receipt ──────────────────────────────────────────────────────────────

export const MAX_RECEIPT_LINES = 6
export const MAX_RECEIPT_LINE_LEN = 64

export function textToLines(text: string): string[] {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l.length > 0)
}

export function linesToText(lines: string[]): string {
  return lines.join('\n')
}

const linesText = z
  .string()
  .refine((t) => textToLines(t).length <= MAX_RECEIPT_LINES, `At most ${MAX_RECEIPT_LINES} lines`)
  .refine((t) => textToLines(t).every((l) => l.length <= MAX_RECEIPT_LINE_LEN), `Each line at most ${MAX_RECEIPT_LINE_LEN} characters`)

export const receiptSchema = z
  .object({
    receipt_paper_width_mm: z.union([z.literal(58), z.literal(80)]),
    receipt_printable_area_mm: z.number({ invalid_type_error: 'Enter a width in mm' }).int().min(40).max(80),
    receipt_logo_url: z.string().max(400_000, 'Image is too large; use one under 300 KB'),
    receipt_header_lines: linesText,
    receipt_footer_lines: linesText,
    receipt_default_document: z.enum(['thermal', 'a4']),
  })
  .refine((v) => v.receipt_printable_area_mm <= v.receipt_paper_width_mm, {
    message: 'Printable area cannot exceed the paper width',
    path: ['receipt_printable_area_mm'],
  })
export type ReceiptFormValues = z.infer<typeof receiptSchema>

export function receiptFromSettings(s: AppSettings): ReceiptFormValues {
  return {
    receipt_paper_width_mm: s.receipt_paper_width_mm,
    receipt_printable_area_mm: s.receipt_printable_area_mm,
    receipt_logo_url: s.receipt_logo_url,
    receipt_header_lines: linesToText(s.receipt_header_lines),
    receipt_footer_lines: linesToText(s.receipt_footer_lines),
    receipt_default_document: s.receipt_default_document,
  }
}

export function receiptToPatch(v: ReceiptFormValues): SettingsPatch {
  return {
    receipt_paper_width_mm: v.receipt_paper_width_mm,
    receipt_printable_area_mm: v.receipt_printable_area_mm,
    receipt_logo_url: v.receipt_logo_url,
    receipt_header_lines: textToLines(v.receipt_header_lines),
    receipt_footer_lines: textToLines(v.receipt_footer_lines),
    receipt_default_document: v.receipt_default_document,
  }
}

// ── Day close, Credit ────────────────────────────────────────────────────

export const dayCloseSchema = z.object({
  day_close_variance_threshold: z.number({ invalid_type_error: 'Enter an amount' }).min(0).max(1_000_000),
})
export type DayCloseFormValues = z.infer<typeof dayCloseSchema>

export const creditSchema = z.object({
  credit_limit_enforced: z.boolean(),
})
export type CreditFormValues = z.infer<typeof creditSchema>
