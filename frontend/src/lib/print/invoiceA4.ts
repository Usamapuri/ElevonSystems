/**
 * The A4 tax invoice — spec §8.3. Same money as the thermal receipt, laid out
 * as a document a registered buyer can file: seller block, buyer block with
 * NTN/CNIC and registration type, an itemised table and a totals table.
 *
 * Pure, like the other builders: (invoice, settings) in, finished HTML out.
 *
 * On the buyer's registration type — the invoice payload carries the buyer's
 * NTN and CNIC but not `buyer_registration_type` (see
 * `backend/internal/models/invoice.go`), so this file infers it from the NTN
 * and lets a caller that already holds the full customer record override both
 * that and the address rather than printing a field it guessed at.
 */
import type { AppSettings, BuyerRegistrationType, Invoice, InvoiceLine } from '@/types'
import { formatMoney } from '@/lib/money'
import { formatKg } from '@/lib/weight'
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

/** What the invoice payload does not carry. Every field is optional. */
export interface A4BuyerDetails {
  address?: string | null
  province?: string | null
  buyer_registration_type?: BuyerRegistrationType | null
}

export function buildInvoiceA4Html(invoice: Invoice, settings: AppSettings, buyer: A4BuyerDetails = {}): string {
  const body = [
    sellerHeader(invoice, settings),
    partyBlocks(invoice, settings, buyer),
    lineTable(invoice.lines ?? []),
    totalsTable(invoice),
    tenderBlock(invoice),
    footerBlock(settings),
    invoice.status === 'voided' ? '<div class="void-stamp">VOID</div>' : '',
  ].join('\n')

  return [
    '<!DOCTYPE html>',
    '<html lang="en"><head><meta charset="utf-8" />',
    '<title>' + esc('Tax invoice ' + invoice.invoice_number) + '</title>',
    '<style>' + A4_CSS + '</style>',
    '</head><body><div class="page">',
    body,
    '</div></body></html>',
  ].join('\n')
}

// ── sections ──────────────────────────────────────────────────────────────

function sellerHeader(inv: Invoice, s: AppSettings): string {
  const left: string[] = ['<div class="brand">']
  if (s.receipt_logo_url) left.push('<img class="logo" src="' + esc(s.receipt_logo_url) + '" alt="" />')
  left.push('<div class="biz">' + esc(s.business_name || 'Elevon POS') + '</div>')
  if (s.business_address) left.push('<div class="muted">' + esc(s.business_address) + '</div>')
  if (s.business_phone) left.push('<div class="muted">' + esc(s.business_phone) + '</div>')
  left.push('</div>')

  const right = [
    '<div class="docmeta">',
    '<div class="doctitle">TAX INVOICE</div>',
    '<table class="kv">',
    kv('Invoice no.', inv.invoice_number),
    kv('Date', formatDateTimePK(inv.created_at)),
    kv('Business day', formatBusinessDate(inv.business_date)),
    kv('Cashier', inv.cashier_name),
    inv.status === 'voided' ? kv('Status', 'VOIDED') : '',
    '</table>',
    '</div>',
  ].join('\n')

  return '<div class="headrow">' + left.join('\n') + right + '</div><div class="hr"></div>'
}

function partyBlocks(inv: Invoice, s: AppSettings, buyer: A4BuyerDetails): string {
  const seller = [
    '<div class="party">',
    '<div class="party-title">Seller</div>',
    '<div class="party-name">' + esc(s.business_name || 'Elevon POS') + '</div>',
    s.business_address ? '<div>' + esc(s.business_address) + '</div>' : '',
    s.business_province ? '<div>' + esc(s.business_province) + '</div>' : '',
    s.business_ntn ? '<div>NTN: ' + esc(s.business_ntn) + '</div>' : '',
    s.business_strn ? '<div>STRN: ' + esc(s.business_strn) + '</div>' : '',
    '</div>',
  ].join('\n')

  const registration: BuyerRegistrationType =
    buyer.buyer_registration_type ?? (inv.customer_ntn ? 'Registered' : 'Unregistered')

  const buyerRows = inv.customer_name
    ? [
        '<div class="party-name">' + esc(inv.customer_name) + '</div>',
        buyer.address ? '<div>' + esc(buyer.address) + '</div>' : '',
        buyer.province ? '<div>' + esc(buyer.province) + '</div>' : '',
        inv.customer_phone ? '<div>' + esc(inv.customer_phone) + '</div>' : '',
        inv.customer_ntn ? '<div>NTN: ' + esc(inv.customer_ntn) + '</div>' : '',
        inv.customer_cnic ? '<div>CNIC: ' + esc(inv.customer_cnic) + '</div>' : '',
        '<div>Registration: ' + esc(registration) + '</div>',
      ]
    : ['<div class="party-name">Walk-in customer</div>', '<div>Registration: Unregistered</div>']

  const buyerBlock = ['<div class="party">', '<div class="party-title">Buyer</div>', ...buyerRows, '</div>'].join('\n')
  return '<div class="parties">' + seller + buyerBlock + '</div>'
}

function lineTable(lines: InvoiceLine[]): string {
  const rows = lines.map((line, i) => {
    const weights =
      line.gross_weight !== null && line.tare_weight !== null
        ? '<div class="muted">Before fill ' +
          esc(formatKg(line.tare_weight)) +
          ' kg, after fill ' +
          esc(formatKg(line.gross_weight)) +
          ' kg</div>'
        : ''
    return [
      '<tr>',
      '<td class="num">' + (i + 1) + '</td>',
      '<td>' + esc(line.product_name) + weights + '</td>',
      '<td>' + esc(line.hs_code ?? '') + '</td>',
      '<td class="num">' + esc(formatKg(line.quantity)) + ' ' + esc(line.fbr_uom ?? 'kg') + '</td>',
      '<td class="num">' + esc(amount2(line.unit_price)) + '</td>',
      '<td class="num">' + esc(amount2(line.line_discount)) + '</td>',
      '<td class="num">' + esc(amount2(line.line_tax)) + '</td>',
      '<td class="num">' + esc(amount2(line.line_total)) + '</td>',
      '</tr>',
    ].join('')
  })

  return [
    '<table class="lines">',
    '<thead><tr>',
    '<th class="num">#</th><th>Description</th><th>HS code</th><th class="num">Quantity</th>',
    '<th class="num">Rate</th><th class="num">Discount</th><th class="num">Tax</th><th class="num">Amount</th>',
    '</tr></thead>',
    '<tbody>' + (rows.length > 0 ? rows.join('\n') : '<tr><td colspan="8" class="muted">No lines</td></tr>') + '</tbody>',
    '</table>',
  ].join('\n')
}

function totalsTable(inv: Invoice): string {
  const rows: string[] = [total('Subtotal', amount2(inv.subtotal))]
  if (inv.discount_amount !== 0) {
    const label = inv.discount_percent ? 'Discount (' + plainPercentLabel(inv.discount_percent) + ')' : 'Discount'
    rows.push(total(label, '-' + amount2(inv.discount_amount)))
  }
  rows.push(total('Sales tax (' + percentLabel(inv.tax_rate) + ')', amount2(inv.tax_amount)))
  if (inv.further_tax_amount > 0) rows.push(total('Further tax', amount2(inv.further_tax_amount)))
  rows.push(total('Total', amount2(inv.total_amount)))
  if (inv.rounding_adjustment !== 0) rows.push(total('Rounding', signedAmount2(inv.rounding_adjustment)))
  rows.push('<tr class="grand"><td>Total payable</td><td class="num">' + esc(formatMoney(inv.total_payable)) + '</td></tr>')
  return '<div class="totalswrap"><table class="totals">' + rows.join('\n') + '</table></div>'
}

function tenderBlock(inv: Invoice): string {
  const sub = subMethodLabel(inv.payment_sub_method)
  const rows = [kv('Paid by', paymentMethodLabel(inv.payment_method) + (sub ? ' — ' + sub : ''))]
  if (inv.payment_reference) rows.push(kv('Reference', inv.payment_reference))
  if (inv.payment_method === 'credit' && inv.customer_balance_after !== null) {
    rows.push(kv('Account balance', formatMoney(inv.customer_balance_after)))
  }
  if (inv.status === 'voided') {
    rows.push(kv('Voided', formatDateTimePK(inv.voided_at)))
    if (inv.void_reason) rows.push(kv('Void reason', inv.void_reason))
  }
  return '<div class="tender"><table class="kv">' + rows.join('\n') + '</table></div>'
}

function footerBlock(s: AppSettings): string {
  const lines = escapedLines(s.receipt_footer_lines)
  if (lines.length === 0) return ''
  return '<div class="hr"></div><div class="foot">' + lines.join('<br />') + '</div>'
}

// ── helpers ───────────────────────────────────────────────────────────────

function kv(label: string, value: string | null | undefined): string {
  return '<tr><td class="k">' + esc(label) + '</td><td class="v">' + esc(value) + '</td></tr>'
}

function total(label: string, value: string): string {
  return '<tr><td>' + esc(label) + '</td><td class="num">' + esc(value) + '</td></tr>'
}

const A4_CSS = [
  '@page { size: A4; margin: 12mm; }',
  '* { box-sizing: border-box; }',
  'html, body { margin: 0; padding: 0; background: #fff; color: #111;',
  '  font-family: "Segoe UI", Arial, Helvetica, sans-serif; font-size: 10.5pt; line-height: 1.4;',
  '  -webkit-print-color-adjust: exact; print-color-adjust: exact; }',
  '.page { position: relative; width: 100%; }',
  '.headrow { display: flex; justify-content: space-between; align-items: flex-start; gap: 10mm; }',
  '.logo { max-height: 20mm; max-width: 55mm; display: block; margin-bottom: 2mm; }',
  '.biz { font-size: 16pt; font-weight: 700; text-transform: uppercase; }',
  '.muted { color: #555; font-size: 9pt; }',
  '.docmeta { min-width: 70mm; }',
  '.doctitle { font-size: 15pt; font-weight: 700; letter-spacing: 1pt; text-align: right; margin-bottom: 2mm; }',
  '.hr { border-top: 1.5pt solid #111; margin: 4mm 0; }',
  '.parties { display: flex; gap: 6mm; margin-bottom: 5mm; }',
  '.party { flex: 1; border: 0.6pt solid #999; padding: 3mm; font-size: 9.5pt; }',
  '.party-title { font-size: 8.5pt; text-transform: uppercase; letter-spacing: 0.6pt; color: #555; margin-bottom: 1.5mm; }',
  '.party-name { font-weight: 700; font-size: 11pt; }',
  'table { border-collapse: collapse; width: 100%; }',
  '.kv td { font-size: 9.5pt; padding: 0.6mm 0; }',
  '.kv td.k { color: #555; white-space: nowrap; padding-right: 4mm; }',
  '.kv td.v { text-align: right; }',
  '.lines th, .lines td { border: 0.6pt solid #999; padding: 1.8mm 2mm; font-size: 9.5pt; vertical-align: top; }',
  '.lines th { background: #eee; text-align: left; font-size: 8.5pt; text-transform: uppercase; letter-spacing: 0.4pt; }',
  '.lines .num, td.num, th.num { text-align: right; white-space: nowrap; }',
  '.totalswrap { display: flex; justify-content: flex-end; margin-top: 4mm; }',
  '.totals { width: 85mm; }',
  '.totals td { padding: 1mm 2mm; font-size: 10pt; }',
  '.totals tr.grand td { border-top: 1.2pt solid #111; font-size: 12.5pt; font-weight: 700; padding-top: 2mm; }',
  '.tender { margin-top: 5mm; max-width: 95mm; }',
  '.foot { font-size: 9pt; color: #555; text-align: center; }',
  '.void-stamp { position: absolute; top: 38%; left: 8%; right: 8%; text-align: center;',
  '  font-size: 72pt; font-weight: 900; letter-spacing: 8pt; color: #111; opacity: 0.16;',
  '  border: 4pt solid #111; transform: rotate(-18deg); pointer-events: none; }',
].join('\n')
