/**
 * The Z-report — the end-of-day slip, printed on the same thermal roll as the
 * receipts so it can be torn off and stapled to the cash count.
 *
 * `buildZReportHtml` is pure, like the other builders.
 *
 * One judgement worth stating: where the day has been closed, the tender table
 * prints the SEALED `expected_*` from the row, not the live recomputation.
 * They should be identical; when they are not, something changed after the
 * seal, and a slip that silently switched to the live number would hide
 * exactly the fact the owner needs to see. The live figure is what an open
 * day (an X-read) has to use, since nothing is sealed yet.
 */
import type { AppSettings, CashMovement, ZReport } from '@/types'
import { formatMoney } from '@/lib/money'
import { fontScaleFor, thermalPrintBaseCss } from '@/lib/print/thermalPrintCss'
import { doc } from '@/lib/print/receipt'
import { amount2, esc, formatBusinessDate, formatDateTimePK, formatTimePK, signedAmount2 } from '@/lib/print/format'

export function buildZReportHtml(z: ZReport, settings: AppSettings): string {
  const paperWidthMm = settings.receipt_paper_width_mm ?? 80
  const printableAreaMm = settings.receipt_printable_area_mm ?? paperWidthMm - 8
  const css =
    thermalPrintBaseCss({ paperWidthMm, printableAreaMm, fontScale: fontScaleFor(paperWidthMm) }) +
    '\n.sec-title { font-weight: 700; text-transform: uppercase; letter-spacing: 0.4mm; margin-top: 1mm; }' +
    '\n.tender th { font-size: 0.85em; text-align: right; border-bottom: 1px solid #000; }' +
    '\n.tender th.k, .tender td.k { text-align: left; }' +
    '\n.tender td { text-align: right; white-space: nowrap; }'

  const sealed = z.day.status !== 'open'
  const body = [
    header(settings),
    '<div class="rule"></div>',
    '<div class="title">' + (sealed ? 'Z-REPORT' : 'X-READ (DAY OPEN)') + '</div>',
    lifecycle(z),
    '<div class="rule"></div>',
    salesSummary(z),
    '<div class="rule"></div>',
    tenderTable(z),
    '<div class="rule"></div>',
    accountsBlock(z),
    movementsBlock(z.movements),
    closingNotes(z),
    footer(z),
  ].join('\n')

  return doc('Z-report ' + z.day.business_date, css, '<div class="thermal-page">' + body + '</div>')
}

// ── sections ──────────────────────────────────────────────────────────────

function header(s: AppSettings): string {
  const out: string[] = ['<div class="center">']
  if (s.receipt_logo_url) out.push('<img class="logo" src="' + esc(s.receipt_logo_url) + '" alt="" />')
  out.push('<div class="biz">' + esc(s.business_name || 'Elevon POS') + '</div>')
  if (s.business_address) out.push('<div class="meta">' + esc(s.business_address) + '</div>')
  if (s.business_ntn) out.push('<div class="meta">NTN: ' + esc(s.business_ntn) + '</div>')
  out.push('</div>')
  return out.join('\n')
}

function lifecycle(z: ZReport): string {
  const d = z.day
  const rows = [
    kv('Business date', formatBusinessDate(d.business_date)),
    kv('Status', d.status.toUpperCase()),
    kv('Opened by', withTime(d.opened_by_name, d.opened_at)),
    kv('Closed by', d.closed_at ? withTime(d.closed_by_name, d.closed_at) : 'not closed'),
    kv('Opening cash', formatMoney(d.opening_cash)),
    kv('Printed', formatDateTimePK(z.generated_at)),
  ]
  return '<table class="kv">' + rows.join('\n') + '</table>'
}

function salesSummary(z: ZReport): string {
  const e = z.expected
  const rows = [
    row('Gross sales', amount2(e.gross_sales)),
    row('Discounts', '-' + amount2(e.discounts)),
    row('Tax collected', amount2(e.tax_collected)),
    row('Net sales', amount2(e.net_sales)),
    row('Invoices', String(e.invoice_count)),
    row('Voids', String(e.void_count)),
  ]
  return '<div class="sec-title">Sales</div><table class="totals">' + rows.join('\n') + '</table>'
}

/**
 * Expected / counted / variance per tender. A day that is still open has
 * nothing counted, so those two columns print a dash rather than a zero — a
 * zero there reads as "counted, and it was empty".
 */
function tenderTable(z: ZReport): string {
  const d = z.day
  const e = z.expected
  const rows = [
    tenderRow('Cash', d.expected_cash ?? e.cash, d.counted_cash, d.cash_variance),
    tenderRow('Card', d.expected_card ?? e.card, d.counted_card, d.card_variance),
    tenderRow('Online', d.expected_online ?? e.online, d.counted_online, d.online_variance),
  ]
  return [
    '<div class="sec-title">Tender</div>',
    '<table class="tender">',
    '<tr><th class="k">Tender</th><th>Expected</th><th>Counted</th><th>Variance</th></tr>',
    rows.join('\n'),
    '</table>',
  ].join('\n')
}

function tenderRow(label: string, expected: number, counted: number | null, variance: number | null): string {
  return (
    '<tr><td class="k">' +
    esc(label) +
    '</td><td>' +
    esc(amount2(expected)) +
    '</td><td>' +
    esc(counted === null ? '—' : amount2(counted)) +
    '</td><td>' +
    esc(variance === null ? '—' : signedAmount2(variance)) +
    '</td></tr>'
  )
}

function accountsBlock(z: ZReport): string {
  const e = z.expected
  const rows = [
    row('On-account sales', amount2(e.on_account_sales)),
    row('Receipts collected', amount2(e.receipts_collected)),
    row('Paid in', amount2(e.paid_in)),
    row('Paid out', '-' + amount2(e.paid_out)),
  ]
  return '<div class="sec-title">Accounts &amp; drawer</div><table class="totals">' + rows.join('\n') + '</table>'
}

function movementsBlock(movements: CashMovement[]): string {
  if (!movements || movements.length === 0) return ''
  const rows = movements.map((m) => {
    const signed = m.movement_type === 'paid_out' ? '-' + amount2(m.amount) : '+' + amount2(m.amount)
    const who = m.created_by_name ? ' · ' + m.created_by_name : ''
    return (
      '<tr><td>' +
      esc(formatTimePK(m.created_at)) +
      ' ' +
      esc(m.reason) +
      '<div class="sub">' +
      esc((m.movement_type === 'paid_out' ? 'Paid out' : 'Paid in') + who) +
      '</div></td><td class="v">' +
      esc(signed) +
      '</td></tr>'
    )
  })
  return (
    '<div class="rule"></div><div class="sec-title">Cash movements</div>' +
    '<table class="totals items">' +
    rows.join('\n') +
    '</table>'
  )
}

function closingNotes(z: ZReport): string {
  if (!z.day.closing_notes) return ''
  return '<div class="rule"></div><div class="sec-title">Closing notes</div><div class="meta">' + esc(z.day.closing_notes) + '</div>'
}

/**
 * Sign-off, then the business date again as a tear-off identifier. The
 * configured `receipt_footer_lines` deliberately do NOT print here: they are
 * the customer's footer ("Thank you", return policy) and this slip goes into
 * the owner's folder, not across the counter.
 */
function footer(z: ZReport): string {
  return (
    '<div class="rule"></div>' +
    '<div class="meta">Counted by ____________________</div>' +
    '<div class="meta">Signature ______________________</div>' +
    '<div class="rule"></div>' +
    '<div class="foot">' +
    esc(formatBusinessDate(z.day.business_date)) +
    '</div>'
  )
}

// ── helpers ───────────────────────────────────────────────────────────────

function kv(label: string, value: string): string {
  return '<tr><td class="k">' + esc(label) + '</td><td class="v">' + esc(value) + '</td></tr>'
}

function row(label: string, value: string): string {
  return '<tr><td>' + esc(label) + '</td><td class="v">' + esc(value) + '</td></tr>'
}

function withTime(name: string | null, at: string | null): string {
  const who = name ?? '—'
  return at ? who + ' · ' + formatTimePK(at) : who
}
