/**
 * Placeholder for Task D8. The till already calls this at every point a
 * receipt should come out (after a charge, from the Reprint button), so
 * when D8 lands — HTML templates, an off-screen iframe with the @page size
 * locked, window.elevon.print() when the desktop bridge is present, see
 * lib/print/transport.ts — the wiring is already in place and only this
 * file changes.
 *
 * It deliberately does NOT call window.print() today: a demo that throws up
 * the browser's print dialog after every sale is worse than one that does
 * not print yet.
 *
 * The signature is the contract D8 must keep.
 */
import type { Invoice, PrintDocument } from '@/types'

export async function printInvoice(invoice: Invoice, document: PrintDocument): Promise<void> {
  console.info(
    `[print] invoice ${invoice.invoice_number} (${document}, Rs ${invoice.total_payable}) — receipt rendering lands in Task D8`,
  )
}
