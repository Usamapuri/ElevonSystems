import { describe, expect, it } from 'vitest'
import { prettyJson, testHeadline } from './fiscal'
import type { FiscalTestResult } from '@/types'

const base: FiscalTestResult = {
  ok: true,
  environment: 'sandbox',
  steps: [],
  request_body: { invoiceType: 'Sale Invoice' },
  invoice_number: '6110180871403DIAJGEJ4031308',
  error_code: '',
}

describe('testHeadline', () => {
  it('reads a sandbox pass as filed', () => {
    expect(testHeadline(base)).toEqual({ passed: true, title: 'Passed', detail: 'FBR accepted the test invoice.' })
  })
  it('reads a production pass as validated only', () => {
    expect(testHeadline({ ...base, environment: 'production', invoice_number: '' }).detail).toBe(
      'FBR validated the test invoice. Nothing was filed.',
    )
  })
  it('explains a refusal made before any call', () => {
    expect(testHeadline({ ...base, ok: false, error_code: 'fiscal_token_missing' }).detail).toBe('Save a token for this environment first.')
    expect(testHeadline({ ...base, ok: false, error_code: 'sandbox_in_release' }).detail).toBe('This server does not allow sandbox filing.')
    expect(testHeadline({ ...base, ok: false, error_code: 'fiscal_hs_code_missing' }).detail).toBe('Set a default HS code in Tax settings first.')
    expect(testHeadline({ ...base, ok: false, error_code: 'fiscal_token_missing' }).passed).toBe(false)
  })
  it('quotes FBR for a rejection', () => {
    const r: FiscalTestResult = {
      ...base,
      ok: false,
      error_code: 'fiscal_rejected',
      steps: [{ phase: 'validate', url: 'x', http_status: 200, status_code: '01', error_code: '0104', error: 'Provided sales tax amount does not match', line: 1, raw_response: {} }],
    }
    expect(testHeadline(r)).toEqual({ passed: false, title: 'Failed', detail: 'FBR rejected it: 0104 — Provided sales tax amount does not match (line 1)' })
  })
  it('names an unreachable gateway', () => {
    const r: FiscalTestResult = {
      ...base,
      ok: false,
      error_code: 'fiscal_unreachable',
      steps: [{ phase: 'validate', url: 'x', http_status: 0, status_code: '', error_code: 'fiscal_unreachable', error: 'FBR could not be reached', line: 0, raw_response: null }],
    }
    expect(testHeadline(r).detail).toBe('FBR could not be reached.')
  })
})

describe('prettyJson', () => {
  it('is stable two-space JSON', () => {
    expect(prettyJson({ b: 1, a: [1, 2] })).toBe('{\n  "b": 1,\n  "a": [\n    1,\n    2\n  ]\n}')
    expect(prettyJson(null)).toBe('null')
  })
})

import { fiscalStatusLabel, ineligibleReason, pendingLine, selectionTotals } from './fiscal'
import type { EligibleInvoice, FiscalPending } from '@/types'

describe('fiscalStatusLabel', () => {
  it('names every status in a cashier’s words', () => {
    expect(fiscalStatusLabel('off')).toBe('Not filed')
    expect(fiscalStatusLabel('unfiled')).toBe('Unfiled')
    expect(fiscalStatusLabel('pending')).toBe('Filing…')
    expect(fiscalStatusLabel('synced')).toBe('Filed')
    expect(fiscalStatusLabel('covered')).toBe('Filed (combined)')
    expect(fiscalStatusLabel('failed')).toBe('Rejected')
    expect(fiscalStatusLabel('weird')).toBe('weird')
  })
})

describe('pendingLine', () => {
  const p: FiscalPending = {
    enabled: true,
    today: { count: 14, kg: 3240.5, total_payable: 812340 },
    all: { count: 14, kg: 3240.5, total_payable: 812340 },
  }
  it('summarises today, and the backlog when it is bigger', () => {
    expect(pendingLine(p)).toBe('Unfiled today: 14 invoices · 3,240.500 kg · Rs 812,340')
    expect(pendingLine({ ...p, all: { ...p.all, count: 31 } })).toBe('Unfiled today: 14 invoices · 3,240.500 kg · Rs 812,340 · 31 in all')
    expect(pendingLine({ ...p, today: { count: 1, kg: 12.5, total_payable: 3909 }, all: { count: 1, kg: 12.5, total_payable: 3909 } })).toBe(
      'Unfiled today: 1 invoice · 12.500 kg · Rs 3,909',
    )
  })
  it('is silent when fiscal is off or nothing is unfiled', () => {
    expect(pendingLine({ ...p, enabled: false })).toBeNull()
    expect(pendingLine({ enabled: true, today: { count: 0, kg: 0, total_payable: 0 }, all: { count: 0, kg: 0, total_payable: 0 } })).toBeNull()
  })
})

describe('selectionTotals / ineligibleReason', () => {
  const row = (id: string, eligible: boolean, kg: number, amount: number, payable: number): EligibleInvoice => ({
    id, invoice_number: id, status: 'completed', created_at: '', customer_name: null,
    total_kg: kg, total_amount: amount, total_payable: payable, fiscal_status: 'unfiled', eligible, reason: eligible ? '' : 'registered_buyer',
  })
  it('sums only rows that are both selected and eligible', () => {
    const rows = [row('a', true, 10, 100, 100), row('b', true, 20, 200, 200), row('c', false, 30, 300, 300)]
    expect(selectionTotals(rows, new Set(['a', 'c']))).toEqual({ count: 1, kg: 10, total_payable: 100 })
    expect(selectionTotals(rows, new Set(['a', 'b']))).toEqual({ count: 2, kg: 30, total_payable: 300 })
  })
  it('explains why a row cannot join', () => {
    expect(ineligibleReason('registered_buyer')).toBe('Registered buyer — file this one on its own')
    expect(ineligibleReason('tax_rate_mismatch')).toBe('Tax rate differs from the FBR rate')
    expect(ineligibleReason('')).toBe('')
  })
})
