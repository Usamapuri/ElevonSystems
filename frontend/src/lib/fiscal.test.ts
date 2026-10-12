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
