/**
 * The 80mm (or 58mm) thermal receipt — spec §8.3.
 *
 * `buildReceiptHtml` is a pure function from (invoice, settings) to a finished
 * HTML document string. It touches no DOM and no clock, which is what lets
 * `receipt.test.ts` assert the whole slip in vitest's node environment, and
 * what lets the desktop bridge print the identical bytes the browser would.
 *
 * Widths come from settings: `receipt_paper_width_mm` is the roll width
 * declared to the driver, `receipt_printable_area_mm` the narrower frame the
 * content is laid out in so nothing reaches the paper's hard right edge.
 */
import type { AppSettings, Invoice, InvoiceLine } from '@/types'
import { formatMoney } from '@/lib/money'
import { formatKg } from '@/lib/weight'
import { fontScaleFor, thermalPrintBaseCss } from '@/lib/print/thermalPrintCss'
import {
  amount2,
  esc,
  escapedLines,
  formatBusinessDate,
  formatDateTimePK,
  paymentMethodLabel,
  percentLabel,
  plainPercentLabel,
  signedAmount2,
  subMethodLabel,
} from '@/lib/print/format'

export function buildReceiptHtml(invoice: Invoice, settings: AppSettings): string {
  const paperWidthMm = settings.receipt_paper_width_mm ?? 80
  const printableAreaMm = settings.receipt_printable_area_mm ?? paperWidthMm - 8
  const css = thermalPrintBaseCss({ paperWidthMm, printableAreaMm, fontScale: fontScaleFor(paperWidthMm) })

  const body = [
    header(settings),
    '<div class="rule"></div>',
    invoiceHead(invoice),
    customerBlock(invoice),
    '<div class="rule"></div>',
    lineItems(invoice.lines ?? []),
    '<div class="rule"></div>',
    totals(invoice),
    '<div class="rule"></div>',
    tender(invoice),
    fiscalBlock(invoice),
    footer(settings),
    invoice.status === 'voided' ? '<div class="void-stamp">VOID</div>' : '',
  ].join('\n')

  return doc('Receipt ' + invoice.invoice_number, css, '<div class="thermal-page">' + body + '</div>')
}

// ── sections ──────────────────────────────────────────────────────────────

function header(s: AppSettings): string {
  const out: string[] = ['<div class="center">']
  if (s.receipt_logo_url) out.push('<img class="logo" src="' + esc(s.receipt_logo_url) + '" alt="" />')
  out.push('<div class="biz">' + esc(s.business_name || 'Elevon POS') + '</div>')
  if (s.business_address) out.push('<div class="meta">' + esc(s.business_address) + '</div>')
  if (s.business_phone) out.push('<div class="meta">' + esc(s.business_phone) + '</div>')
  if (s.business_ntn) out.push('<div class="meta">NTN: ' + esc(s.business_ntn) + '</div>')
  if (s.business_strn) out.push('<div class="meta">STRN: ' + esc(s.business_strn) + '</div>')
  for (const line of escapedLines(s.receipt_header_lines)) out.push('<div class="meta">' + line + '</div>')
  out.push('</div>')
  return out.join('\n')
}

function invoiceHead(inv: Invoice): string {
  return [
    '<div class="title">INVOICE</div>',
    '<div class="center bold">' + esc(inv.invoice_number) + '</div>',
    '<table class="kv">',
    kv('Date', formatDateTimePK(inv.created_at)),
    kv('Business day', formatBusinessDate(inv.business_date)),
    kv('Cashier', inv.cashier_name),
    '</table>',
  ].join('\n')
}

function customerBlock(inv: Invoice): string {
  if (!inv.customer_name) return ''
  const rows = [kv('Customer', inv.customer_name)]
  if (inv.customer_phone) rows.push(kv('Phone', inv.customer_phone))
  if (inv.customer_ntn) rows.push(kv('NTN', inv.customer_ntn))
  if (inv.customer_cnic) rows.push(kv('CNIC', inv.customer_cnic))
  return '<table class="kv">' + rows.join('\n') + '</table>'
}

/**
 * One product per two rows: the name, then `12.500 kg x 265.00` against the
 * line total. A gross-minus-tare line carries the two weighbridge readings on
 * a third row, because that is the number the customer watched being taken.
 */
function lineItems(lines: InvoiceLine[]): string {
  const rows: string[] = ['<table class="items">']
  for (const line of lines) {
    rows.push('<tr><td class="name" colspan="2">' + esc(line.product_name) + '</td></tr>')
    rows.push(
      '<tr><td class="qty">' +
        esc(formatKg(line.quantity)) +
        ' kg &times; ' +
        esc(amount2(line.unit_price)) +
        '</td><td class="amt">' +
        esc(amount2(line.line_total)) +
        '</td></tr>',
    )
    if (line.gross_weight !== null && line.tare_weight !== null) {
      rows.push(
        '<tr><td class="sub" colspan="2">Gross ' +
          esc(formatKg(line.gross_weight)) +
          ' kg &minus; Tare ' +
          esc(formatKg(line.tare_weight)) +
          ' kg</td></tr>',
      )
    }
    if (line.line_discount > 0) {
      rows.push('<tr><td class="sub" colspan="2">Less discount ' + esc(amount2(line.line_discount)) + '</td></tr>')
    }
  }
  rows.push('</table>')
  return rows.join('\n')
}

function totals(inv: Invoice): string {
  const rows: string[] = ['<table class="totals">']
  rows.push(total('Subtotal', amount2(inv.subtotal)))
  if (inv.discount_amount !== 0) {
    const label = inv.discount_percent ? 'Discount (' + plainPercentLabel(inv.discount_percent) + ')' : 'Discount'
    rows.push(total(label, '-' + amount2(inv.discount_amount)))
  }
  rows.push(total('Sales tax (' + percentLabel(inv.tax_rate) + ')', amount2(inv.tax_amount)))
  if (inv.further_tax_amount > 0) rows.push(total('Further tax', amount2(inv.further_tax_amount)))
  // Only when it actually moved the figure — a "Rounding 0.00" line on every
  // receipt is noise the cashier learns to stop reading.
  if (inv.rounding_adjustment !== 0) rows.push(total('Rounding', signedAmount2(inv.rounding_adjustment)))
  rows.push('</table>')
  rows.push('<div class="rule-solid"></div>')
  rows.push(
    '<table class="totals grand"><tr><td>TOTAL</td><td class="v">' + esc(formatMoney(inv.total_payable)) + '</td></tr></table>',
  )
  return rows.join('\n')
}

function tender(inv: Invoice): string {
  const rows: string[] = ['<table class="kv">']
  const sub = subMethodLabel(inv.payment_sub_method)
  rows.push(kv('Paid by', paymentMethodLabel(inv.payment_method) + (sub ? ' — ' + sub : '')))
  if (inv.payment_reference) rows.push(kv('Reference', inv.payment_reference))
  rows.push('</table>')
  if (inv.payment_method === 'credit' && inv.customer_balance_after !== null) {
    rows.push(
      '<div class="center bold">On account — balance now ' + esc(formatMoney(inv.customer_balance_after)) + '</div>',
    )
  }
  if (inv.status === 'voided') {
    rows.push('<div class="rule"></div>')
    rows.push('<div class="center bold">VOIDED' + (inv.voided_at ? ' ' + esc(formatDateTimePK(inv.voided_at)) : '') + '</div>')
    if (inv.void_reason) rows.push('<div class="center meta">Reason: ' + esc(inv.void_reason) + '</div>')
  }
  return rows.join('\n')
}

/**
 * Spec §7.7. Only the `off` case is live today (fiscal lands in Phase 7) and
 * it prints nothing at all — a receipt that claims an FBR relationship the
 * store does not have is worse than one that stays quiet. The other branches
 * are here so that turning fiscal on needs no change to this file.
 */
function fiscalBlock(inv: Invoice): string {
  if (inv.fiscal_status === 'pending' || inv.fiscal_status === 'failed') {
    return '<div class="rule"></div><div class="center meta">FBR submission pending</div>'
  }
  if (inv.fiscal_status === 'synced' && inv.fiscal_invoice_number) {
    return (
      '<div class="rule"></div><div class="center meta">FBR Invoice No. ' + esc(inv.fiscal_invoice_number) + '</div>'
    )
  }
  return ''
}

function footer(s: AppSettings): string {
  const lines = escapedLines(s.receipt_footer_lines)
  if (lines.length === 0) return ''
  return '<div class="rule"></div><div class="foot">' + lines.join('<br />') + '</div>'
}

// ── small helpers ─────────────────────────────────────────────────────────

function kv(label: string, value: string | null | undefined): string {
  return '<tr><td class="k">' + esc(label) + '</td><td class="v">' + esc(value) + '</td></tr>'
}

function total(label: string, value: string): string {
  return '<tr><td>' + esc(label) + '</td><td class="v">' + esc(value) + '</td></tr>'
}

/** The document shell every thermal slip shares. */
export function doc(title: string, css: string, body: string): string {
  return [
    '<!DOCTYPE html>',
    '<html lang="en"><head><meta charset="utf-8" />',
    '<title>' + esc(title) + '</title>',
    '<style>' + css + '</style>',
    '</head><body>',
    body,
    '</body></html>',
  ].join('\n')
}
