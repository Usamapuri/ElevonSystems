export interface APIResponse<T = unknown> {
  success: boolean
  message: string
  data?: T
  error?: string
}

export interface PaginatedResponse<T> {
  success: boolean
  message: string
  data: T[]
  meta: { current_page: number; per_page: number; total: number; total_pages: number }
}

export type Role = 'admin' | 'counter'
export type ThemePreference = 'light' | 'dark' | 'high-contrast'

export interface User {
  id: string
  username: string
  email: string | null
  first_name: string
  last_name: string
  role: Role
  is_active: boolean
  has_pin: boolean
  last_login_at: string | null
  created_at: string
  updated_at: string
}

/** username may be the staff username or their email. */
export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: User
}

/** Mirrors the 21 keys seeded in backend/migrations/001_init.sql. Tax rates are fractions. */
export interface AppSettings {
  business_name: string
  business_address: string
  business_phone: string
  business_ntn: string
  business_strn: string
  business_province: string
  day_boundary_hour: number
  tax_rate_cash: number
  tax_rate_card: number
  tax_rate_online: number
  tax_rate_credit: number | null
  further_tax_rate: number
  default_hs_code: string
  receipt_paper_width_mm: 58 | 80
  receipt_printable_area_mm: number
  receipt_logo_url: string
  receipt_header_lines: string[]
  receipt_footer_lines: string[]
  receipt_default_document: 'thermal' | 'a4'
  day_close_variance_threshold: number
  credit_limit_enforced: boolean
}

export type SettingsPatch = Partial<AppSettings>

export interface UserListParams {
  page?: number
  per_page?: number
  search?: string
  role?: Role
  active?: boolean
}

export interface CreateUserRequest {
  username: string
  email: string
  password: string
  first_name: string
  last_name: string
  role: Role
}

/** Every field optional; omit to leave unchanged. Username is immutable. */
export interface UpdateUserRequest {
  email?: string
  password?: string
  first_name?: string
  last_name?: string
  role?: Role
  is_active?: boolean
}

// ── Products & rates ──────────────────────────────────────────────────────

export interface Product {
  id: string
  name: string
  sku: string | null
  sell_by: 'weight' | 'unit'
  unit_label: string
  rate: number
  hs_code: string | null
  fbr_uom: string | null
  sort_order: number
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface ProductListParams {
  active?: boolean
}

/** sell_by is always 'weight' and unit_label always 'kg' in v1; the server sets both. */
export interface CreateProductRequest {
  name: string
  sku?: string
  rate: number
  hs_code?: string
  fbr_uom?: string
  sort_order?: number
}

/** Every field optional; omit to leave unchanged. No rate here — see UpdateRatesRequest. */
export interface UpdateProductRequest {
  name?: string
  sku?: string
  hs_code?: string
  fbr_uom?: string
  sort_order?: number
  is_active?: boolean
}

export interface RateChange {
  product_id: string
  rate: number
}

export interface UpdateRatesRequest {
  changes: RateChange[]
  note?: string
}

export interface UpdateRatesResponse {
  updated: number
  products: Product[]
}

export interface RateHistoryEntry {
  id: string
  product_id: string
  product_name: string
  old_rate: number
  new_rate: number
  changed_by_name: string | null
  changed_at: string
  note: string | null
}

export interface RateHistoryParams {
  product_id?: string
  limit?: number
}

// ── Customers & ledger ────────────────────────────────────────────────────

export type BuyerRegistrationType = 'Registered' | 'Unregistered'

/** balance = Σ debit − Σ credit; positive means the customer owes money. */
export interface Customer {
  id: string
  name: string
  phone: string | null
  ntn: string | null
  cnic: string | null
  buyer_registration_type: BuyerRegistrationType
  address: string | null
  province: string | null
  credit_allowed: boolean
  credit_limit: number | null
  is_active: boolean
  notes: string | null
  balance: number
  created_at: string
  updated_at: string
}

export interface CustomerListParams {
  page?: number
  per_page?: number
  search?: string
  active?: boolean
}

/** A blank buyer_registration_type defaults to "Unregistered" server-side. */
export interface CreateCustomerRequest {
  name: string
  phone?: string
  ntn?: string
  cnic?: string
  buyer_registration_type?: BuyerRegistrationType
  address?: string
  province?: string
  credit_allowed?: boolean
  credit_limit?: number
  notes?: string
}

/** Every field optional; omit to leave unchanged. There is no way to null
 * out an existing credit_limit through this endpoint — set it to 0. */
export interface UpdateCustomerRequest {
  name?: string
  phone?: string
  ntn?: string
  cnic?: string
  buyer_registration_type?: BuyerRegistrationType
  address?: string
  province?: string
  credit_allowed?: boolean
  credit_limit?: number
  is_active?: boolean
  notes?: string
}

export type LedgerEntryType = 'invoice' | 'invoice_void' | 'receipt' | 'receipt_void' | 'adjustment'

export type ReceiptMethod = 'cash' | 'card' | 'online'

/** One customer_receipts row: money taken against an account, not against a
 * specific invoice (spec §6.5). `customer_balance_after` carries the same
 * meaning as on Invoice — the ledger balance immediately after this receipt
 * (or its void) posted — and is set only on the create/void responses. */
export interface Receipt {
  id: string
  receipt_number: string
  customer_id: string
  customer_name: string
  amount: number
  method: ReceiptMethod
  sub_method: string | null
  reference: string | null
  business_day_id: string
  business_date: string
  received_by: string | null
  received_by_name: string
  note: string | null
  created_at: string
  voided_at: string | null
  voided_by: string | null
  void_reason: string | null
  customer_balance_after: number | null
}

/** POST /customers/:id/receipts. Credit is not a method here — a receipt is
 * money arriving, so it can only be cash, card or online. */
export interface CreateReceiptRequest {
  amount: number
  method: ReceiptMethod
  sub_method?: string
  reference?: string
  note?: string
}

/** POST /customers/:id/receipts/:rid/void. */
export interface VoidReceiptRequest {
  reason: string
  pin: string
}

/** GET /customers/:id/ageing — the receivables report's row for one
 * customer, or (a settled account) a zeroed row carrying only name, phone
 * and the date last paid. Buckets age unsettled debits by business_date,
 * FIFO against receipts (spec §6.8). */
export interface CustomerAgeing {
  customer_id: string
  name: string
  phone: string
  balance: number
  b0_30: number
  b31_60: number
  b61_90: number
  b90: number
  last_receipt: string | null
}

export interface StatementRow {
  id: string
  business_date: string
  entry_type: LedgerEntryType
  invoice_number: string | null
  receipt_number: string | null
  debit: number
  credit: number
  running_balance: number
  note: string | null
}

export interface StatementParams {
  from?: string
  to?: string
}

// ── Business day (the till only reads it; /day-close owns the writes) ─────

export type DayStatus = 'open' | 'closed' | 'reopened'

/** One business_days row. Every nullable number is null until the day is
 * closed — the till reads status and business_date and nothing else. */
export interface BusinessDay {
  id: string
  business_date: string
  status: DayStatus
  opened_at: string
  opened_by: string | null
  opened_by_name: string | null
  opening_cash: number
  opening_notes: string | null
  closed_at: string | null
  closed_by: string | null
  closed_by_name: string | null
  counted_cash: number | null
  counted_card: number | null
  counted_online: number | null
  expected_cash: number | null
  expected_card: number | null
  expected_online: number | null
  cash_variance: number | null
  card_variance: number | null
  online_variance: number | null
  gross_sales: number | null
  discounts: number | null
  tax_collected: number | null
  net_sales: number | null
  on_account_sales: number | null
  receipts_collected: number | null
  invoice_count: number | null
  void_count: number | null
  closing_notes: string | null
  created_at: string
  updated_at: string
}

/** Live recomputation of what should be in the drawer. */
export interface DayExpected {
  opening_cash: number
  cash_sales: number
  card_sales: number
  online_sales: number
  on_account_sales: number
  cash_receipts: number
  card_receipts: number
  online_receipts: number
  receipts_collected: number
  paid_in: number
  paid_out: number
  cash: number
  card: number
  online: number
  gross_sales: number
  discounts: number
  tax_collected: number
  net_sales: number
  invoice_count: number
  void_count: number
}

export interface CashMovement {
  id: string
  business_day_id: string
  movement_type: 'paid_in' | 'paid_out'
  amount: number
  reason: string
  notes: string | null
  created_by: string | null
  created_by_name: string | null
  created_at: string
}

/** GET /day/:id/z — everything the printable Z slip needs in one round-trip.
 * `day` carries the counted amounts and variances (null until the day is
 * closed); `expected` is recomputed live, so on a sealed day the two agree and
 * any divergence means rows changed after the seal. */
export interface ZReport {
  generated_at: string
  day: BusinessDay
  expected: DayExpected
  movements: CashMovement[]
}

/** GET /day/current. `day` is the day holding the open slot; failing that,
 * today's row even when it is already closed (a sale then reopens it for a
 * late sale, §6.7 branch 3); null only when today has never been opened. */
export interface DayCurrent {
  day: BusinessDay | null
  expected: DayExpected | null
  movements: CashMovement[]
}

// ── Invoices ──────────────────────────────────────────────────────────────

export type PaymentMethod = 'cash' | 'card' | 'online' | 'credit'
export type InvoiceStatus = 'completed' | 'voided'
/** How the cashier rang the line up; matches invoice_lines_entered_as_check. */
export type EnteredAs = 'kg' | 'tonne' | 'amount' | 'gross_tare'
export type PrintDocument = 'thermal' | 'a4'

/** Every money field was computed server-side from products.rate — none of
 * them can be set by a client (see backend/internal/models/invoice.go). */
export interface InvoiceLine {
  id: string
  invoice_id: string
  product_id: string | null
  product_name: string
  hs_code: string | null
  fbr_uom: string | null
  quantity: number
  entered_as: EnteredAs | null
  gross_weight: number | null
  tare_weight: number | null
  unit_price: number
  line_total: number
  line_discount: number
  line_tax: number
  sort_order: number
}

/** `lines` is omitted from listings and present on the single reads and on
 * the create/void responses. `total_payable` is whole rupees — the figure
 * the customer pays. `customer_balance_after` is set only on the responses
 * that moved a balance. */
export interface Invoice {
  id: string
  invoice_number: string
  client_op_id: string | null
  business_day_id: string
  business_date: string
  status: InvoiceStatus
  cashier_id: string | null
  cashier_name: string
  customer_id: string | null
  customer_name: string | null
  customer_phone: string | null
  customer_ntn: string | null
  customer_cnic: string | null
  subtotal: number
  discount_amount: number
  discount_percent: number | null
  tax_rate: number
  tax_amount: number
  further_tax_amount: number
  total_amount: number
  rounding_adjustment: number
  total_payable: number
  payment_method: PaymentMethod
  payment_reference: string | null
  payment_sub_method: string | null
  notes: string | null
  fiscal_status: string
  fiscal_invoice_number: string | null
  created_at: string
  voided_at: string | null
  voided_by: string | null
  void_reason: string | null
  lines?: InvoiceLine[]
  customer_balance_after: number | null
}

/** One cart line as submitted. There is deliberately no money field: the
 * server prices every line from products.rate. */
export interface InvoiceLineRequest {
  product_id: string
  quantity: number
  entered_as: EnteredAs
  gross_weight?: number
  tare_weight?: number
}

/** POST /invoices — create and settle in one call. `client_op_id` is the
 * till's idempotency key: a repeat POST with the same UUID returns the
 * invoice already created (200) rather than ringing the sale twice, which
 * is what makes the retry-with-PIN after credit_limit_exceeded safe. `pin`
 * is the admin credit-limit override and is never stored. */
export interface CreateInvoiceRequest {
  client_op_id: string
  lines: InvoiceLineRequest[]
  discount_amount: number
  discount_percent?: number | null
  customer_id?: string
  payment_method: PaymentMethod
  payment_sub_method?: string
  payment_reference?: string
  notes?: string
  pin?: string
}

export interface VoidInvoiceRequest {
  reason: string
  pin: string
}

export interface RecentInvoiceParams {
  limit?: number
}

// ── Day close writes and the invoice browser's filters ────────────────────

/** POST /day/open. `opening_cash` is required and is not defaulted: zero is
 * a real answer a person typed, an omitted field is not, and the server
 * refuses the second (backend models.OpenDayRequest). */
export interface OpenDayRequest {
  opening_cash: number
  notes?: string
}

/** POST /day/movements — one paid-in or paid-out against the open day. */
export interface CashMovementRequest {
  type: 'paid_in' | 'paid_out'
  amount: number
  reason: string
  notes?: string
}

/** POST /day/close. All three counts are required — enter 0 for a tender
 * that took nothing, so a null counted column can only ever mean
 * "force-closed, nobody counted". 400 `variance_note_required` when a
 * tender is off by more than `day_close_variance_threshold` and
 * `closing_notes` is blank. */
export interface CloseDayRequest {
  counted_cash: number
  counted_card: number
  counted_online: number
  closing_notes?: string
}

/** POST /admin/day/reopen. Without `day_id` it means today. */
export interface ReopenDayRequest {
  pin: string
  day_id?: string
}

/** POST /admin/day/force-close — seals a day nobody counted. Without
 * `day_id` it means whichever day holds the open slot. */
export interface ForceCloseDayRequest {
  pin: string
  reason: string
  day_id?: string
}

export interface DayHistoryParams {
  limit?: number
}

/** GET /invoices. `from`/`to` are bare `YYYY-MM-DD` business dates — the
 * server filters on `business_date`, never on `created_at`. */
export interface InvoiceListParams {
  from?: string
  to?: string
  search?: string
  customer_id?: string
  cashier_id?: string
  payment_method?: PaymentMethod
  status?: InvoiceStatus
  page?: number
  per_page?: number
}

// ── Dashboard & reports (GET /admin/dashboard, GET /admin/reports/:name) ──

/** Money by how it arrived. OnAccount is credit sales — billed, not
 * collected — so it never belongs in a drawer count, but it is still part
 * of `net` (backend/internal/reports/summary.go): the four fields partition
 * `PeriodSummary.net` exactly. */
export interface TenderSplit {
  cash: number
  card: number
  online: number
  on_account: number
}

/** The one set of figures behind the dashboard KPIs, the daily report's
 * totals row and the Excel period pack (spec §6.8). `from`/`to` are ISO
 * dates. `net = taxable + tax + further_tax + rounding` by construction. */
export interface PeriodSummary {
  from: string
  to: string
  invoices: number
  voids: number
  kg_sold: number
  gross: number
  discount: number
  taxable: number
  tax: number
  further_tax: number
  rounding: number
  net: number
  tenders: TenderSplit
  receipts: TenderSplit
  receipts_total: number
}

/** One business date's summary — the embedded PeriodSummary carries
 * `from === to === business_date`. `label` is `DD-MM-YYYY`. */
export interface DailyRow extends PeriodSummary {
  business_date: string
  label: string
}

/** One product's slice of a window, biggest gross first. `product_id` is
 * null when the product has since been deleted; `share` is a percentage of
 * the window's gross. */
export interface ProductRow {
  product_id: string | null
  name: string
  kg: number
  invoices: number
  gross: number
  share: number
}

/** The dashboard's recent-sales row — deliberately not the full Invoice
 * shape (no lines, no fiscal columns): the dashboard lists ten sales and
 * polls every 30s. */
export interface DashboardInvoice {
  id: string
  invoice_number: string
  business_date: string
  status: InvoiceStatus
  cashier_name: string
  customer_name: string | null
  payment_method: PaymentMethod
  total_payable: number
  created_at: string
}

/** GET /admin/dashboard — the whole screen in one round trip (spec §3).
 * `top_products` is the last 30 days, not today — label it as such.
 *
 * No day field: the day banner lives in shell/PageHeader, which runs its own
 * ['day','current'] query, so the dashboard payload does not carry a second
 * copy for nothing to read. */
export interface DashboardResponse {
  today: PeriodSummary
  receivables_outstanding: number
  series_7d: DailyRow[]
  series_30d: DailyRow[]
  top_products: ProductRow[]
  recent_invoices: DashboardInvoice[]
}

// ── Reports (GET /admin/reports/:name) — Daily/Products/Tax/Cashiers/Hourly/
//    Receivables/Day closes tabs (spec §6.8) ───────────────────────────────

/** `:name` in `GET /admin/reports/:name`; also the CSV/xlsx export name and
 * the `report_not_found` gate (backend/internal/handlers/reports.go
 * reportSheets). */
export type ReportName = 'daily' | 'products' | 'tax' | 'cashiers' | 'hourly' | 'receivables' | 'day-closes'

/** Both bare `YYYY-MM-DD` business dates; the server defaults each to today
 * when omitted (`invalid_range` on a reversed or >366-day window). */
export interface ReportParams {
  from?: string
  to?: string
}

/** GET /admin/reports/:name in JSON. `totals` is null for `receivables` and
 * `day-closes` — a balance as of a date and a set of sealed rows are not a
 * period to sum (backend/internal/handlers/reports.go reportView). `cells`
 * is only ever present on `hourly` — the heatmap grid alongside the flat
 * `rows` every report carries; every other report simply omits the field. */
export interface ReportResponse<T> {
  rows: T[]
  totals: PeriodSummary | null
  cells?: HeatCell[]
}

/** One effective tax-rate band over the window (backend
 * internal/reports/queries.go TaxBand). `rate` is a fraction (0.18),
 * matching `settings.tax_rate_*` — never a bare percentage; render it with
 * `percentLabel`. Σ TaxBand.tax over every band equals the window's
 * PeriodSummary.tax by construction. */
export interface TaxBand {
  rate: number
  invoices: number
  taxable: number
  tax: number
  further_tax: number
}

/** One cashier's window, biggest gross first. `cashier_id` is null when the
 * user row was deleted; the invoice's own `cashier_name` snapshot names
 * them. `average` is gross ÷ completed invoices for that cashier. */
export interface CashierRow {
  cashier_id: string | null
  name: string
  invoices: number
  gross: number
  average: number
  voids: number
}

/** One hour-of-day bucket, 0–23 in the business timezone. All 24 hours are
 * always present, including empty ones, so a chart has a fixed x-axis. */
export interface HourRow {
  hour: number
  invoices: number
  net: number
}

/** One hour × weekday bucket of the Hourly report's heatmap (backend
 * internal/reports/queries.go HeatCell; Task P8). `weekday` is 0=Mon…6=Sun
 * (ISODOW − 1). All 7×24 = 168 cells are always present, zero-filled, so the
 * grid never has to guess at a missing (weekday, hour) pair. */
export interface HeatCell {
  weekday: number
  hour: number
  invoices: number
  net: number
}

/** One customer's outstanding account as of the window's `to` date — a
 * position, not a period. The four buckets age the *unpaid* part of the
 * balance (FIFO by business_date) and sum to `balance` whenever it is
 * positive (backend internal/reports/queries.go Receivables). Same shape
 * as `CustomerAgeing` but for every customer with a non-zero balance. */
export interface ReceivableRow {
  customer_id: string
  name: string
  phone: string
  balance: number
  b0_30: number
  b31_60: number
  b61_90: number
  b90: number
  last_receipt: string | null
}

/** One business day's close row (backend internal/reports/queries.go
 * DayCloseRow). An open or reopened day in the window is listed with its
 * variance columns at zero rather than hidden. `net` is the sealed
 * net_sales for a closed day — what the Z-report printed. */
export interface DayCloseRow {
  id: string
  business_date: string
  label: string
  status: DayStatus
  closed_by: string
  expected_cash: number
  counted_cash: number
  cash_variance: number
  card_variance: number
  online_variance: number
  net: number
}
