/**
 * FBR filing helpers shared by Settings → FBR, the till and the File with
 * FBR screen (spec 2026-10-12 §2). Pure: no DOM, no React, so the node
 * vitest suite covers every branch.
 */
import { formatKgGrouped, formatMoney } from '@/lib/money'
import type { EligibleInvoice, FiscalPending, FiscalTestResult, PendingSummary } from '@/types'

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

/** invoices.fiscal_status in a cashier's words. Unknown values pass through. */
export function fiscalStatusLabel(status: string): string {
  switch (status) {
    case 'off':
      return 'Not filed'
    case 'unfiled':
      return 'Unfiled'
    case 'pending':
      return 'Filing…'
    case 'synced':
      return 'Filed'
    case 'covered':
      return 'Filed (combined)'
    case 'failed':
      return 'Rejected'
    default:
      return status
  }
}

/** The one quiet line under the till's totals. null = say nothing. */
export function pendingLine(p: FiscalPending): string | null {
  if (!p.enabled || (p.today.count === 0 && p.all.count === 0)) return null
  const t = p.today
  let line = `Unfiled today: ${t.count} ${t.count === 1 ? 'invoice' : 'invoices'} · ${formatKgGrouped(t.kg)} · ${formatMoney(t.total_payable)}`
  if (p.all.count > t.count) line += ` · ${p.all.count} in all`
  return line
}

/** What the ticked, eligible rows come to. */
export function selectionTotals(rows: EligibleInvoice[], selected: Set<string>): PendingSummary {
  let count = 0
  let kg = 0
  let total_payable = 0
  for (const r of rows) {
    if (!r.eligible || !selected.has(r.id)) continue
    count += 1
    kg += r.total_kg
    total_payable += r.total_payable
  }
  return { count, kg: Math.round(kg * 1000) / 1000, total_payable }
}

export function ineligibleReason(reason: EligibleInvoice['reason']): string {
  switch (reason) {
    case 'registered_buyer':
      return 'Registered buyer — file this one on its own'
    case 'tax_rate_mismatch':
      return 'Tax rate differs from the FBR rate'
    case 'already_filed':
      return 'FBR already holds this sale'
    default:
      return ''
  }
}
