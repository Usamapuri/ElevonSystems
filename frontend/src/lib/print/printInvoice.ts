/**
 * Dispatch: build a document with a pure builder, hand it to the one transport.
 *
 * `printInvoice(invoice, document)` keeps the signature D7 pinned — the till
 * already calls it after every charge and behind every Reprint button, so
 * nothing outside this file changed when D8 landed.
 *
 * The `document` argument wins. Callers pass the settings default
 * (`receipt_default_document`) when the operator has not asked for something
 * else, which is why this file does not second-guess it; settings are fetched
 * here only for the things a builder cannot do without — paper width, business
 * identity, header and footer lines.
 *
 * Nothing here throws. A receipt that fails to print must never look like a
 * sale that failed to record: the sale is already committed server-side by the
 * time we get here, so a print failure is logged and swallowed.
 */
import type { AppSettings, Invoice, PrintDocument, ZReport } from '@/types'
import apiClient from '@/api/client'
import { buildReceiptHtml } from '@/lib/print/receipt'
import { buildInvoiceA4Html } from '@/lib/print/invoiceA4'
import { buildZReportHtml } from '@/lib/print/zReport'
import { printDebug } from '@/lib/print/printDebug'
import { pageSizeForWidth, printHtml } from '@/lib/print/transport'

/**
 * Enough of AppSettings to print with when the settings call fails. Deliberately
 * blank rather than invented: an unnamed receipt is honest, a receipt with a
 * placeholder business name and someone else's NTN is not.
 */
const FALLBACK_SETTINGS: AppSettings = {
  business_name: '',
  business_address: '',
  business_phone: '',
  business_ntn: '',
  business_strn: '',
  business_province: '',
  day_boundary_hour: 0,
  tax_rate_cash: 0,
  tax_rate_card: 0,
  tax_rate_online: 0,
  tax_rate_credit: null,
  further_tax_rate: 0,
  default_hs_code: '',
  receipt_paper_width_mm: 80,
  receipt_printable_area_mm: 72,
  receipt_logo_url: '',
  receipt_header_lines: [],
  receipt_footer_lines: [],
  receipt_default_document: 'thermal',
  day_close_variance_threshold: 0,
  credit_limit_enforced: false,
}

/**
 * Settings are read fresh per job rather than cached: a reprint after the
 * owner widened the printable area or fixed a typo in the footer should come
 * out right, and one extra GET per sale is nothing next to the print itself.
 */
async function loadSettings(): Promise<AppSettings> {
  try {
    const res = await apiClient.getSettings()
    if (res.success && res.data) return res.data
    printDebug.warn('settings', 'settings call returned no data — printing with blanks')
  } catch (err) {
    printDebug.error('settings', 'settings call failed — printing with blanks', err)
  }
  return FALLBACK_SETTINGS
}

/** Print one invoice as a thermal receipt or an A4 tax invoice. */
export async function printInvoice(invoice: Invoice, document: PrintDocument): Promise<void> {
  const settings = await loadSettings()
  try {
    if (document === 'a4') {
      await printHtml(buildInvoiceA4Html(invoice, settings), { pageSize: 'a4' })
      return
    }
    await printHtml(buildReceiptHtml(invoice, settings), {
      pageSize: pageSizeForWidth(settings.receipt_paper_width_mm),
      printableAreaMm: settings.receipt_printable_area_mm,
    })
  } catch (err) {
    printDebug.error('invoice', 'printing invoice ' + invoice.invoice_number + ' failed', err)
  }
}

/** Print the end-of-day Z slip. Settings are passed in by the day-close screen. */
export async function printZReport(z: ZReport, settings: AppSettings): Promise<void> {
  try {
    await printHtml(buildZReportHtml(z, settings), {
      pageSize: pageSizeForWidth(settings.receipt_paper_width_mm),
      printableAreaMm: settings.receipt_printable_area_mm,
    })
  } catch (err) {
    printDebug.error('zreport', 'printing the Z-report for ' + z.day.business_date + ' failed', err)
  }
}
