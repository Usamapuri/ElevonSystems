/**
 * FBR filing helpers shared by Settings → FBR, the till and the File with
 * FBR screen (spec 2026-10-12 §2). Pure: no DOM, no React, so the node
 * vitest suite covers every branch.
 */
import type { FiscalTestResult } from '@/types'

export interface TestHeadline {
  passed: boolean
  title: 'Passed' | 'Failed'
  detail: string
}

/** The refusals RunTest makes before any HTTP, in a cashier's words. */
const REFUSALS: Record<string, string> = {
  fiscal_token_missing: 'Save a token for this environment first.',
  sandbox_in_release: 'This server does not allow sandbox filing.',
  fiscal_hs_code_missing: 'Set a default HS code in Tax settings first.',
  fiscal_secrets_key_missing: 'The server has no FISCAL_SECRETS_KEY, so the stored token cannot be read.',
  fiscal_unreachable: 'FBR could not be reached.',
  fiscal_no_invoice_number: 'FBR accepted the invoice but returned no number.',
}

/** One line for the top of the test-result dialog. */
export function testHeadline(r: FiscalTestResult): TestHeadline {
  if (r.ok) {
    return {
      passed: true,
      title: 'Passed',
      detail: r.environment === 'production' ? 'FBR validated the test invoice. Nothing was filed.' : 'FBR accepted the test invoice.',
    }
  }
  const rejected = r.steps.find((s) => s.error_code !== '' && s.error_code !== 'fiscal_unreachable')
  if (r.error_code === 'fiscal_rejected' && rejected) {
    const line = rejected.line > 0 ? ` (line ${rejected.line})` : ''
    return { passed: false, title: 'Failed', detail: `FBR rejected it: ${rejected.error_code} — ${rejected.error}${line}` }
  }
  return { passed: false, title: 'Failed', detail: REFUSALS[r.error_code] ?? `The test failed (${r.error_code || 'unknown'}).` }
}

/** Stable two-space JSON for the dialog's <pre> blocks and the download. */
export function prettyJson(v: unknown): string {
  return JSON.stringify(v, null, 2)
}
