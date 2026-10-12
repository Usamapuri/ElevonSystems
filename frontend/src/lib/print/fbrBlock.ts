/**
 * The FBR block on a receipt or A4 invoice (spec §7.7, 2026-10-12 §2.6):
 * the FBR invoice number and its QR, only on a sale FBR accepted on its
 * own. A sale covered by a combined invoice prints as an ordinary receipt —
 * the combined number belongs to the batch, not to this slip — and an
 * unfiled, pending or failed sale says nothing: the till tells the cashier.
 *
 * Pure and DOM-free so receipt.test.ts can run it in node.
 */
import { esc } from '@/lib/print/format'
import { FBR_DI_LOGO_DATA_URL } from '@/lib/print/fbrLogo'

export interface FbrBlockSource {
  fiscal_status: string
  fiscal_invoice_number: string | null
}

export function fbrBlockHtml(inv: FbrBlockSource, qrDataUrl: string | null): string {
  if (inv.fiscal_status !== 'synced' || !inv.fiscal_invoice_number) return ''
  const parts = [
    '<div class="fbr">',
    '<img src="' + FBR_DI_LOGO_DATA_URL + '" alt="FBR Digital Invoicing System" class="fbr-logo" />',
    '<div class="fbr-label">FBR Invoice No.</div>',
    '<div class="fbr-number">' + esc(inv.fiscal_invoice_number) + '</div>',
  ]
  if (qrDataUrl) parts.push('<img src="' + esc(qrDataUrl) + '" alt="FBR verification QR" class="fbr-qr" />')
  parts.push('</div>')
  return parts.join('')
}
