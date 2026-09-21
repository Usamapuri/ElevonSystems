/**
 * The receipt builder, asserted against the §8.3 checklist.
 *
 * These are substring assertions rather than a whole-file snapshot on purpose.
 * A snapshot of ~4 KB of HTML fails on every CSS tweak and teaches you to
 * press `-u` without reading, which is the opposite of what a receipt test is
 * for. What must not regress is *what the slip says* — the invoice number, the
 * `12.500 kg x 265.00` line, the rounding line only when it is non-zero, the
 * on-account balance, the VOID stamp — so those are what is pinned here.
 */
import { describe, expect, it } from 'vitest'
import { buildReceiptHtml } from './receipt'
import { buildInvoiceA4Html } from './invoiceA4'
import { buildZReportHtml } from './zReport'
import type { AppSettings, BusinessDay, DayExpected, Invoice, InvoiceLine, ZReport } from '@/types'

const settings: AppSettings = {
  business_name: 'Shahzad Gas & Co',
  business_address: 'Plot 14, Multan Road, Lahore',
  business_phone: '042-3555-0100',
  business_ntn: '1234567-8',
  business_strn: '03-05-8888-001-99',
  business_province: 'Punjab',
  day_boundary_hour: 4,
  tax_rate_cash: 0.18,
  tax_rate_card: 0.18,
  tax_rate_online: 0.18,
  tax_rate_credit: null,
  further_tax_rate: 0.04,
  default_hs_code: '',
  receipt_paper_width_mm: 80,
  receipt_printable_area_mm: 72,
  receipt_logo_url: '',
  receipt_header_lines: ['LPG — domestic & commercial'],
  receipt_footer_lines: ['Thank you', 'Keep the cylinder upright'],
  receipt_default_document: 'thermal',
  day_close_variance_threshold: 200,
  credit_limit_enforced: true,
}

/** The money below is the §6.3 arithmetic worked through, not decoration:
 * a print test that carries impossible totals teaches you to stop reading
 * them. Cash sale: 12.500 kg x 265.00 = 3,312.50, less a Rs 12.37 amount
 * discount, 18% tax on 3,300.13 = 594.02, total 3,894.15, payable 3,894,
 * rounding -0.15. */
const cashLine: InvoiceLine = {
  id: 'l1',
  invoice_id: 'i1',
  product_id: 'p1',
  product_name: 'LPG bulk',
  hs_code: '2711.1910',
  fbr_uom: 'KG',
  quantity: 12.5,
  entered_as: 'kg',
  gross_weight: null,
  tare_weight: null,
  unit_price: 265,
  line_total: 3312.5,
  line_discount: 12.37,
  line_tax: 594.02,
  sort_order: 0,
}

const cashInvoice: Invoice = {
  id: 'i1',
  invoice_number: '20260920-007',
  client_op_id: 'c1',
  business_day_id: 'd1',
  business_date: '2026-09-20',
  status: 'completed',
  cashier_id: 'u1',
  cashier_name: 'Adeel Raza',
  customer_id: null,
  customer_name: null,
  customer_phone: null,
  customer_ntn: null,
  customer_cnic: null,
  subtotal: 3312.5,
  discount_amount: 12.37,
  discount_percent: null,
  tax_rate: 0.18,
  tax_amount: 594.02,
  further_tax_amount: 0,
  total_amount: 3894.15,
  rounding_adjustment: -0.15,
  total_payable: 3894,
  payment_method: 'cash',
  payment_reference: null,
  payment_sub_method: null,
  notes: null,
  fiscal_status: 'off',
  fiscal_invoice_number: null,
  created_at: '2026-09-20T10:45:00+05:00',
  voided_at: null,
  voided_by: null,
  void_reason: null,
  lines: [cashLine],
  customer_balance_after: null,
}

/** Credit sale, two lines, 5% discount pro-rated with the last line absorbing
 * the paisa remainder: subtotal 6,280.50, discount 314.03 (165.63 + 148.40),
 * tax 1,073.97, total 7,040.44, payable 7,040, rounding -0.44. */
const creditLineA: InvoiceLine = { ...cashLine, line_discount: 165.63, line_tax: 566.44 }

const creditLineB: InvoiceLine = {
  ...cashLine,
  id: 'l2',
  product_name: 'LPG cylinder refill',
  quantity: 11.2,
  entered_as: 'gross_tare',
  gross_weight: 26.4,
  tare_weight: 15.2,
  line_total: 2968,
  line_discount: 148.4,
  line_tax: 507.53,
  sort_order: 1,
}

const creditInvoice: Invoice = {
  ...cashInvoice,
  id: 'i2',
  invoice_number: '20260920-008',
  customer_id: 'cust1',
  customer_name: 'Hameed Traders',
  customer_phone: '0300-1112233',
  customer_ntn: '7654321-0',
  customer_cnic: '35201-1234567-1',
  subtotal: 6280.5,
  discount_amount: 314.03,
  discount_percent: 5,
  tax_amount: 1073.97,
  total_amount: 7040.44,
  rounding_adjustment: -0.44,
  total_payable: 7040,
  payment_method: 'credit',
  lines: [creditLineA, creditLineB],
  customer_balance_after: 48250,
}

const voidedInvoice: Invoice = {
  ...cashInvoice,
  id: 'i3',
  invoice_number: '20260920-009',
  status: 'voided',
  voided_at: '2026-09-20T11:05:00+05:00',
  voided_by: 'u9',
  void_reason: 'Wrong product rung up',
}

describe('buildReceiptHtml — header and identity', () => {
  const html = buildReceiptHtml(cashInvoice, settings)

  it('prints the business identity and the configured header lines', () => {
    expect(html).toContain('Shahzad Gas &amp; Co')
    expect(html).toContain('Plot 14, Multan Road, Lahore')
    expect(html).toContain('042-3555-0100')
    expect(html).toContain('NTN: 1234567-8')
    expect(html).toContain('STRN: 03-05-8888-001-99')
    expect(html).toContain('LPG — domestic &amp; commercial')
  })

  it('prints INVOICE, the number, the cashier and a Karachi timestamp', () => {
    expect(html).toContain('>INVOICE<')
    expect(html).toContain('20260920-007')
    expect(html).toContain('Adeel Raza')
    // 10:45 +05:00 is 10:45 in Asia/Karachi — the receipt must not shift it.
    expect(html).toContain('20 Sep 2026, 10:45 am')
  })

  it('formats the business date off the bare string, never through Date()', () => {
    expect(html).toContain('20 Sep 2026')
  })

  it('prints the configured footer lines', () => {
    expect(html).toContain('Thank you')
    expect(html).toContain('Keep the cylinder upright')
  })

  it('locks the declared page width to the settings paper width', () => {
    expect(html).toContain('@page { size: 80mm auto; margin: 0; }')
    expect(html).toContain('width: 72mm')
  })
})

describe('buildReceiptHtml — lines and money', () => {
  it('prints a weight line as "12.500 kg x 265.00" against its total', () => {
    const html = buildReceiptHtml(cashInvoice, settings)
    expect(html).toContain('12.500 kg &times; 265.00')
    expect(html).toContain('3,312.50')
  })

  it('prints the before-fill and after-fill weights under a weighed line', () => {
    const html = buildReceiptHtml(creditInvoice, settings)
    expect(html).toContain('Before fill 15.200 kg, after fill 26.400 kg')
  })

  it('prints subtotal, discount and the tax rate', () => {
    const html = buildReceiptHtml(cashInvoice, settings)
    expect(html).toContain('Subtotal')
    expect(html).toContain('<tr><td>Discount</td><td class="v">-12.37</td></tr>')
    expect(html).toContain('Sales tax (18%)')
  })

  it('labels a percentage discount with its percentage', () => {
    expect(buildReceiptHtml(creditInvoice, settings)).toContain('Discount (5%)')
  })

  it('prints the rounding line only when the adjustment is non-zero', () => {
    expect(buildReceiptHtml(cashInvoice, settings)).toContain('<tr><td>Rounding</td><td class="v">-0.15</td></tr>')
    expect(buildReceiptHtml({ ...cashInvoice, rounding_adjustment: 0.35 }, settings)).toContain('+0.35')
    expect(
      buildReceiptHtml({ ...cashInvoice, rounding_adjustment: 0, total_payable: 3894.15 }, settings),
    ).not.toContain('Rounding')
  })

  it('prints further tax only when it was charged', () => {
    expect(buildReceiptHtml(cashInvoice, settings)).not.toContain('Further tax')
    const withFurther = buildReceiptHtml({ ...cashInvoice, further_tax_amount: 132.5 }, settings)
    expect(withFurther).toContain('Further tax')
    expect(withFurther).toContain('132.50')
  })

  it('prints TOTAL as the payable rupee figure', () => {
    expect(buildReceiptHtml(cashInvoice, settings)).toContain('TOTAL')
    expect(buildReceiptHtml({ ...cashInvoice, total_payable: 3909 }, settings)).toContain('Rs 3,909')
  })
})

describe('buildReceiptHtml — tender and customer', () => {
  it('prints the tender, and the sub-method and reference when present', () => {
    const online = buildReceiptHtml(
      { ...cashInvoice, payment_method: 'online', payment_sub_method: 'easypaisa', payment_reference: 'EP-99217' },
      settings,
    )
    expect(online).toContain('Online — Easypaisa')
    expect(online).toContain('EP-99217')
  })

  it('prints the customer with NTN and CNIC on a credit sale', () => {
    const html = buildReceiptHtml(creditInvoice, settings)
    expect(html).toContain('Hameed Traders')
    expect(html).toContain('0300-1112233')
    expect(html).toContain('7654321-0')
    expect(html).toContain('35201-1234567-1')
  })

  it('prints the on-account balance line for a credit sale', () => {
    expect(buildReceiptHtml(creditInvoice, settings)).toContain('On account — balance now Rs 48,250')
  })

  it('omits the on-account line for a cash sale', () => {
    expect(buildReceiptHtml(cashInvoice, settings)).not.toContain('On account')
  })
})

describe('buildReceiptHtml — fiscal block (§7.7)', () => {
  it('says nothing at all when fiscal is off', () => {
    const html = buildReceiptHtml(cashInvoice, settings)
    expect(html).not.toContain('FBR')
  })

  it('says "FBR submission pending" once a submission is queued', () => {
    expect(buildReceiptHtml({ ...cashInvoice, fiscal_status: 'pending' }, settings)).toContain('FBR submission pending')
  })

  it('prints the FBR invoice number once synced', () => {
    const html = buildReceiptHtml(
      { ...cashInvoice, fiscal_status: 'synced', fiscal_invoice_number: '7000007DI1747119701593' },
      settings,
    )
    expect(html).toContain('FBR Invoice No. 7000007DI1747119701593')
  })
})

describe('buildReceiptHtml — void', () => {
  const html = buildReceiptHtml(voidedInvoice, settings)

  it('stamps VOID across the slip and states the reason', () => {
    expect(html).toContain('<div class="void-stamp">VOID</div>')
    expect(html).toContain('Reason: Wrong product rung up')
  })

  it('leaves a completed invoice unstamped', () => {
    expect(buildReceiptHtml(cashInvoice, settings)).not.toContain('<div class="void-stamp">')
  })
})

describe('escaping', () => {
  it('escapes every interpolated string, including free-text void reasons', () => {
    const nasty = buildReceiptHtml(
      {
        ...voidedInvoice,
        customer_name: '<script>alert(1)</script>',
        void_reason: '</style><b>oops</b>',
        lines: [{ ...cashLine, product_name: 'Gas & "Air"' }],
      },
      { ...settings, business_name: 'A & B <Ltd>' },
    )
    expect(nasty).not.toContain('<script>')
    expect(nasty).toContain('&lt;script&gt;')
    expect(nasty).toContain('&lt;/style&gt;')
    expect(nasty).toContain('Gas &amp; &quot;Air&quot;')
    expect(nasty).toContain('A &amp; B &lt;Ltd&gt;')
  })
})

describe('buildInvoiceA4Html', () => {
  it('declares A4 and prints a seller and buyer block', () => {
    const html = buildInvoiceA4Html(creditInvoice, settings)
    expect(html).toContain('@page { size: A4; margin: 12mm; }')
    expect(html).toContain('TAX INVOICE')
    expect(html).toContain('>Seller<')
    expect(html).toContain('>Buyer<')
    expect(html).toContain('Hameed Traders')
    expect(html).toContain('NTN: 7654321-0')
    expect(html).toContain('CNIC: 35201-1234567-1')
  })

  it('infers Registered from the buyer NTN and takes an explicit override', () => {
    expect(buildInvoiceA4Html(creditInvoice, settings)).toContain('Registration: Registered')
    expect(buildInvoiceA4Html(cashInvoice, settings)).toContain('Registration: Unregistered')
    expect(
      buildInvoiceA4Html(creditInvoice, settings, { buyer_registration_type: 'Unregistered', address: 'Ferozepur Road' }),
    ).toContain('Registration: Unregistered')
  })

  it('itemises the lines and totals to the payable figure', () => {
    const html = buildInvoiceA4Html(creditInvoice, settings)
    expect(html).toContain('LPG bulk')
    expect(html).toContain('2711.1910')
    expect(html).toContain('Total payable')
    expect(html).toContain('Rs 7,040')
  })

  it('prints the before-fill and after-fill weights under a weighed line', () => {
    // The gross/tare pair is the scale reading before and after the fill
    // (docs/superpowers: the P5 tare ruling), and the quantity column is the
    // net in the line's FBR unit — 26.400 − 15.200 = 11.200.
    const html = buildInvoiceA4Html(creditInvoice, settings)
    expect(html).toContain('<div class="muted">Before fill 15.200 kg, after fill 26.400 kg</div>')
    expect(html).toContain('11.200 KG')
    // A line typed straight in kg carries no weight note at all.
    expect(buildInvoiceA4Html(cashInvoice, settings)).not.toContain('Before fill')
  })

  it('stamps VOID on a voided invoice', () => {
    expect(buildInvoiceA4Html(voidedInvoice, settings)).toContain('<div class="void-stamp">VOID</div>')
  })
})

// ── Z-report ──────────────────────────────────────────────────────────────

const expected: DayExpected = {
  opening_cash: 5000,
  cash_sales: 42000,
  card_sales: 8000,
  online_sales: 3000,
  on_account_sales: 15000,
  cash_receipts: 2500,
  card_receipts: 0,
  online_receipts: 500,
  receipts_collected: 3000,
  paid_in: 1000,
  paid_out: 750,
  cash: 49750,
  card: 8000,
  online: 3500,
  gross_sales: 68000,
  discounts: 500,
  tax_collected: 10215,
  net_sales: 68000,
  invoice_count: 34,
  void_count: 1,
}

const closedDay: BusinessDay = {
  id: 'd1',
  business_date: '2026-09-20',
  status: 'closed',
  opened_at: '2026-09-20T07:02:00+05:00',
  opened_by: 'u1',
  opened_by_name: 'Adeel Raza',
  opening_cash: 5000,
  opening_notes: null,
  closed_at: '2026-09-20T21:40:00+05:00',
  closed_by: 'u2',
  closed_by_name: 'Usama Puri',
  counted_cash: 49700,
  counted_card: 8000,
  counted_online: 3500,
  expected_cash: 49750,
  expected_card: 8000,
  expected_online: 3500,
  cash_variance: -50,
  card_variance: 0,
  online_variance: 0,
  gross_sales: 68000,
  discounts: 500,
  tax_collected: 10215,
  net_sales: 68000,
  on_account_sales: 15000,
  receipts_collected: 3000,
  invoice_count: 34,
  void_count: 1,
  closing_notes: 'Fifty rupees short — checked twice',
  created_at: '2026-09-20T07:02:00+05:00',
  updated_at: '2026-09-20T21:40:00+05:00',
}

const z: ZReport = {
  generated_at: '2026-09-20T21:41:00+05:00',
  day: closedDay,
  expected,
  movements: [
    {
      id: 'm1',
      business_day_id: 'd1',
      movement_type: 'paid_out',
      amount: 750,
      reason: 'Diesel for the delivery van',
      notes: null,
      created_by: 'u1',
      created_by_name: 'Adeel Raza',
      created_at: '2026-09-20T14:20:00+05:00',
    },
  ],
}

describe('buildZReportHtml', () => {
  const html = buildZReportHtml(z, settings)

  it('titles a sealed day Z-REPORT and an open day an X-read', () => {
    expect(html).toContain('Z-REPORT')
    expect(buildZReportHtml({ ...z, day: { ...closedDay, status: 'open' } }, settings)).toContain('X-READ (DAY OPEN)')
  })

  it('prints the business date, lifecycle and opening cash', () => {
    expect(html).toContain('20 Sep 2026')
    expect(html).toContain('Adeel Raza')
    expect(html).toContain('Usama Puri')
    expect(html).toContain('Rs 5,000')
  })

  it('prints the sales summary including invoice and void counts', () => {
    expect(html).toContain('Gross sales')
    expect(html).toContain('68,000.00')
    expect(html).toContain('Discounts')
    expect(html).toContain('Tax collected')
    expect(html).toContain('>34<')
    expect(html).toContain('>1<')
  })

  it('prints expected / counted / variance per tender', () => {
    expect(html).toContain('49,750.00')
    expect(html).toContain('49,700.00')
    expect(html).toContain('-50.00')
  })

  it('dashes counted and variance while the day is still open', () => {
    const open = buildZReportHtml(
      {
        ...z,
        day: { ...closedDay, status: 'open', counted_cash: null, cash_variance: null, expected_cash: null },
      },
      settings,
    )
    expect(open).toContain('—')
  })

  it('prints on-account sales, receipts and the cash movements', () => {
    expect(html).toContain('On-account sales')
    expect(html).toContain('15,000.00')
    expect(html).toContain('Receipts collected')
    expect(html).toContain('Diesel for the delivery van')
    expect(html).toContain('-750.00')
  })

  it('prints the closing notes', () => {
    expect(html).toContain('Fifty rupees short — checked twice')
  })
})
