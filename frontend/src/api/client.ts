import axios, { type AxiosInstance, type AxiosRequestConfig } from 'axios'
import type {
  APIResponse, AppSettings, BusinessDay, CashMovement, CashMovementRequest, CloseDayRequest, CreateCustomerRequest,
  CreateInvoiceRequest, CreateProductRequest, CreateReceiptRequest, CreateUserRequest, Customer, CustomerAgeing,
  CustomerListParams, DashboardResponse, DayCurrent, DayHistoryParams, ForceCloseDayRequest, Invoice,
  InvoiceListParams, LoginRequest, LoginResponse, OpenDayRequest, PaginatedResponse, Product, ProductListParams,
  RateHistoryEntry, RateHistoryParams, Receipt, RecentInvoiceParams, ReopenDayRequest, ReportName, ReportParams,
  ReportResponse, SettingsPatch, StatementParams, StatementRow, UpdateCustomerRequest, UpdateProductRequest,
  UpdateRatesRequest, UpdateRatesResponse, UpdateUserRequest, User, UserListParams, VoidInvoiceRequest,
  VoidReceiptRequest, ZReport,
} from '@/types'
import { downloadBlob, parseContentDispositionFilename } from '@/lib/download'

export const TOKEN_KEY = 'elevon_token'
export const USER_KEY = 'elevon_user'

/** Thrown on every non-2xx response. Callers branch on `code`, never on message text. */
export class ApiClientError extends Error {
  code?: string
  status?: number
  /** No HTTP response at all (offline, DNS, timeout). */
  isNetworkError?: boolean
  constructor(message: string) {
    super(message)
    this.name = 'ApiClientError'
  }
}

/** Requests use `/auth/...`, `/admin/...`; the base must end in /api/v1. */
export function resolveApiBaseUrl(raw: string | undefined): string {
  const trimmed = typeof raw === 'string' ? raw.trim() : ''
  const base = (trimmed || 'http://localhost:8080/api/v1').replace(/\/+$/, '')
  return /\/api\/v1$/i.test(base) ? base : `${base}/api/v1`
}

/** 401 codes that mean the *session* is over: emitted only by
 * `backend/internal/middleware/auth.go` and the handlers' "Not signed in"
 * branches (`auth_required`). Everything else that answers 401 — PIN gates
 * (`invalid_pin`), a stripped proxy header (`missing_auth_header`), a bad
 * login attempt (`invalid_credentials`) — is not a reason to end the
 * session, so it must stay off this list. */
const SESSION_EXPIRY_CODES = new Set([
  'invalid_auth_format',
  'invalid_token',
  'token_revoked',
  'user_inactive',
  'auth_check_failed',
  'auth_required',
])

export function isSessionExpiryCode(code: string | undefined): boolean {
  return code !== undefined && SESSION_EXPIRY_CODES.has(code)
}

/**
 * Unwraps an axios error response body to the `APIResponse` the server
 * actually sent, whether it arrived as already-parsed JSON (the normal
 * case) or as a `Blob` — any request made with `responseType: 'blob'`
 * (`downloadReport`) gets its error body back as a `Blob` too, even though
 * the server still wrote the usual `{message, error}` JSON into it. Returns
 * undefined when there is nothing recoverable (no response, or a body that
 * is not JSON at all — an HTML proxy error page, say).
 */
async function parseErrorResponseBody(data: unknown): Promise<APIResponse | undefined> {
  if (data instanceof Blob) {
    if (!data.type || !data.type.toLowerCase().includes('json')) return undefined
    try {
      return JSON.parse(await data.text()) as APIResponse
    } catch {
      return undefined
    }
  }
  return data as APIResponse | undefined
}

/**
 * The session-expiry code off an axios error response, unwrapping a `Blob`
 * body first (see `parseErrorResponseBody`). Without this, a 401 on a
 * `responseType: 'blob'` request — a report export with an expired token —
 * reads its code as `undefined` and never ends the session: the interceptor
 * below and `downloadReport`'s own catch block both go through this one
 * function so a blob error body is unwrapped exactly once, the same way,
 * everywhere.
 */
export async function errorCodeFromResponse(response: { data?: unknown } | undefined): Promise<string | undefined> {
  const parsed = await parseErrorResponseBody(response?.data)
  return parsed?.error
}

class APIClient {
  private client: AxiosInstance

  constructor() {
    this.client = axios.create({
      baseURL: resolveApiBaseUrl(import.meta.env?.VITE_API_URL),
      timeout: 30000,
      headers: { 'Content-Type': 'application/json' },
    })
    this.client.interceptors.request.use((config) => {
      const token = localStorage.getItem(TOKEN_KEY)
      if (token) {
        config.headers.Authorization = `Bearer ${token}`
        config.headers['X-POS-JWT'] = token // survives proxies that strip Authorization
      }
      return config
    })
    this.client.interceptors.response.use(
      (r) => r,
      async (error) => {
        if (error.response?.status === 401) {
          // async so a blob-bodied 401 (any report download,
          // responseType: 'blob') is unwrapped the same as a JSON one —
          // see errorCodeFromResponse.
          const code = await errorCodeFromResponse(error.response)
          // Only a genuinely expired/invalid session ends it. PIN gates
          // (invalid_pin), a stripped proxy header (missing_auth_header) and
          // a bad login attempt (invalid_credentials) must not log the
          // cashier out or drop an in-progress cart.
          if (isSessionExpiryCode(code)) {
            this.clearAuth()
            if (window.location.pathname !== '/login') window.location.href = '/login'
          }
        }
        return Promise.reject(error)
      },
    )
  }

  private async request<T>(config: AxiosRequestConfig): Promise<APIResponse<T>> {
    try {
      const res = await this.client.request<APIResponse<T>>(config)
      return res.data
    } catch (err) {
      if (axios.isAxiosError(err)) {
        const data = err.response?.data as APIResponse | undefined
        const e = new ApiClientError(data?.message || err.message || 'Request failed')
        e.code = data?.error
        e.status = err.response?.status
        e.isNetworkError = !err.response
        throw e
      }
      throw err
    }
  }

  private async requestPaginated<T>(config: AxiosRequestConfig): Promise<PaginatedResponse<T>> {
    try {
      const res = await this.client.request<PaginatedResponse<T>>(config)
      return res.data
    } catch (err) {
      if (axios.isAxiosError(err)) {
        const data = err.response?.data as APIResponse | undefined
        const e = new ApiClientError(data?.message || err.message || 'Request failed')
        e.code = data?.error
        e.status = err.response?.status
        e.isNetworkError = !err.response
        throw e
      }
      throw err
    }
  }

  // ── Auth ──────────────────────────────────────────────────────────────
  login(req: LoginRequest) {
    return this.request<LoginResponse>({ method: 'POST', url: '/auth/login', data: req })
  }
  getCurrentUser() {
    return this.request<User>({ method: 'GET', url: '/auth/me' })
  }
  forgotPassword(email: string) {
    return this.request<never>({ method: 'POST', url: '/auth/forgot-password', data: { email } })
  }
  resetPassword(token: string, newPassword: string) {
    return this.request<never>({ method: 'POST', url: '/auth/reset-password', data: { token, new_password: newPassword } })
  }
  changePassword(currentPassword: string, newPassword: string) {
    return this.request<never>({
      method: 'POST',
      url: '/auth/change-password',
      data: { current_password: currentPassword, new_password: newPassword },
    })
  }

  // ── Settings ──────────────────────────────────────────────────────────
  getSettings() {
    return this.request<AppSettings>({ method: 'GET', url: '/settings' })
  }
  updateSettings(patch: SettingsPatch) {
    return this.request<AppSettings>({ method: 'PUT', url: '/admin/settings', data: patch })
  }

  // ── Users (admin) ─────────────────────────────────────────────────────
  getUsers(params: UserListParams = {}) {
    return this.requestPaginated<User>({ method: 'GET', url: '/admin/users', params })
  }
  createUser(req: CreateUserRequest) {
    return this.request<User>({ method: 'POST', url: '/admin/users', data: req })
  }
  updateUser(id: string, req: UpdateUserRequest) {
    return this.request<User>({ method: 'PUT', url: `/admin/users/${id}`, data: req })
  }
  setUserPin(id: string, pin: string) {
    return this.request<never>({ method: 'PUT', url: `/admin/users/${id}/pin`, data: { pin } })
  }

  // ── Products & rates (reads for any staff, writes admin only) ──────────
  getProducts(params: ProductListParams = {}) {
    return this.request<Product[]>({ method: 'GET', url: '/products', params })
  }
  createProduct(req: CreateProductRequest) {
    return this.request<Product>({ method: 'POST', url: '/admin/products', data: req })
  }
  updateProduct(id: string, req: UpdateProductRequest) {
    return this.request<Product>({ method: 'PUT', url: `/admin/products/${id}`, data: req })
  }
  updateRates(req: UpdateRatesRequest) {
    return this.request<UpdateRatesResponse>({ method: 'PUT', url: '/admin/rates', data: req })
  }
  getRateHistory(params: RateHistoryParams = {}) {
    return this.request<RateHistoryEntry[]>({ method: 'GET', url: '/admin/rates/history', params })
  }

  // ── Customers & ledger (reads for any staff, writes admin only) ────────
  getCustomers(params: CustomerListParams = {}) {
    return this.requestPaginated<Customer>({ method: 'GET', url: '/customers', params })
  }
  getCustomer(id: string) {
    return this.request<Customer>({ method: 'GET', url: `/customers/${id}` })
  }
  getCustomerStatement(id: string, params: StatementParams = {}) {
    return this.request<StatementRow[]>({ method: 'GET', url: `/customers/${id}/statement`, params })
  }
  createCustomer(req: CreateCustomerRequest) {
    return this.request<Customer>({ method: 'POST', url: '/admin/customers', data: req })
  }
  updateCustomer(id: string, req: UpdateCustomerRequest) {
    return this.request<Customer>({ method: 'PUT', url: `/admin/customers/${id}`, data: req })
  }

  // ── Receipts (any staff — spec §3: counter may receive payment) ────────
  getReceipts(customerId: string) {
    return this.request<Receipt[]>({ method: 'GET', url: `/customers/${customerId}/receipts` })
  }
  /** 201 with `customer_balance_after`. 409 `day_not_open` / `previous_day_open`,
   * 404 `customer_not_found`, 409 `customer_inactive`. */
  createReceipt(customerId: string, req: CreateReceiptRequest) {
    return this.request<Receipt>({ method: 'POST', url: `/customers/${customerId}/receipts`, data: req })
  }
  /** The PIN is the admin authority to reverse a receipt; never stored. 401
   * `invalid_pin`, 409 `receipt_already_voided`. */
  voidReceipt(customerId: string, receiptId: string, req: VoidReceiptRequest) {
    return this.request<Receipt>({ method: 'POST', url: `/customers/${customerId}/receipts/${receiptId}/void`, data: req })
  }
  getCustomerAgeing(customerId: string) {
    return this.request<CustomerAgeing>({ method: 'GET', url: `/customers/${customerId}/ageing` })
  }

  // ── Business day (the till reads it to know whether it may sell) ───────
  getDayCurrent() {
    return this.request<DayCurrent>({ method: 'GET', url: '/day/current' })
  }
  /** The printable Z slip for one day: the row, the live expectation and the
   * drawer movements in one round-trip. */
  getZReport(dayId: string) {
    return this.request<ZReport>({ method: 'GET', url: `/day/${dayId}/z` })
  }
  /** Declares the cash in the drawer and starts today. 409 `day_already_open`
   * or `previous_day_open` when a day is already holding the slot. */
  openDay(req: OpenDayRequest) {
    return this.request<BusinessDay>({ method: 'POST', url: '/day/open', data: req })
  }
  addMovement(req: CashMovementRequest) {
    return this.request<CashMovement>({ method: 'POST', url: '/day/movements', data: req })
  }
  /** Counts and seals the open day. 400 `variance_note_required`, 409
   * `day_not_open` / `day_closed`. */
  closeDay(req: CloseDayRequest) {
    return this.request<BusinessDay>({ method: 'POST', url: '/day/close', data: req })
  }
  /** Admin PIN paths. Both omit `day_id` to mean "the obvious day": today
   * for a reopen, whichever day holds the open slot for a force close. */
  reopenDay(req: ReopenDayRequest) {
    return this.request<BusinessDay>({ method: 'POST', url: '/admin/day/reopen', data: req })
  }
  forceCloseDay(req: ForceCloseDayRequest) {
    return this.request<BusinessDay>({ method: 'POST', url: '/admin/day/force-close', data: req })
  }
  getDayHistory(params: DayHistoryParams = {}) {
    return this.request<BusinessDay[]>({ method: 'GET', url: '/admin/day/history', params })
  }

  // ── Invoices (the money core; voids carry an admin PIN in the body) ────
  createInvoice(req: CreateInvoiceRequest) {
    return this.request<Invoice>({ method: 'POST', url: '/invoices', data: req })
  }
  getRecentInvoices(params: RecentInvoiceParams = {}) {
    return this.request<Invoice[]>({ method: 'GET', url: '/invoices/recent', params })
  }
  /** The invoice browser. Listings carry no `lines` — a reprint re-reads the
   * invoice with getInvoice first. */
  getInvoices(params: InvoiceListParams = {}) {
    return this.requestPaginated<Invoice>({ method: 'GET', url: '/invoices', params })
  }
  getInvoice(id: string) {
    return this.request<Invoice>({ method: 'GET', url: `/invoices/${id}` })
  }
  voidInvoice(id: string, req: VoidInvoiceRequest) {
    return this.request<Invoice>({ method: 'POST', url: `/invoices/${id}/void`, data: req })
  }

  // ── Dashboard (admin-only) ──────────────────────────────────────────────
  /** Today's KPIs, the 7d/30d revenue+kg series, the last 30 days' top
   * products, recent invoices and the day banner, in one round trip. The
   * dashboard route polls this every 30s. */
  getDashboard() {
    return this.request<DashboardResponse>({ method: 'GET', url: '/admin/dashboard' })
  }

  // ── Reports (admin-only, spec §6.8) ─────────────────────────────────────
  /** GET /admin/reports/:name in JSON. `data.totals` is null for
   * `receivables` and `day-closes`. 400 `invalid_range`, 404
   * `report_not_found`. */
  getReport<T>(name: ReportName, params: ReportParams = {}) {
    return this.request<ReportResponse<T>>({ method: 'GET', url: `/admin/reports/${name}`, params })
  }

  /**
   * Streams a CSV or `.xlsx` export and saves it under the filename the
   * server names in `Content-Disposition` (exposed via CORS —
   * backend/main.go `ExposeHeaders`). `pack` is only meaningful for
   * `daily`+`xlsx`: the six-sheet period pack. This bypasses `request()`
   * because the success path is a file save, not JSON — but a failure still
   * comes back as the usual `APIResponse` body, just wrapped in a blob, so
   * it is unwrapped the same way before being thrown as an `ApiClientError`.
   */
  async downloadReport(
    name: ReportName,
    params: ReportParams & { format: 'csv' | 'xlsx'; pack?: boolean },
  ): Promise<void> {
    try {
      const res = await this.client.request<Blob>({
        method: 'GET',
        url: `/admin/reports/${name}`,
        responseType: 'blob',
        params: {
          from: params.from,
          to: params.to,
          format: params.format,
          ...(params.pack ? { pack: 1 } : {}),
        },
      })
      const filename = parseContentDispositionFilename(res.headers['content-disposition']) ?? `${name}.${params.format}`
      downloadBlob(res.data, filename)
    } catch (err) {
      if (axios.isAxiosError(err)) {
        const parsed = await parseErrorResponseBody(err.response?.data)
        const e = new ApiClientError(parsed?.message || err.message || 'Export failed')
        e.code = parsed?.error
        e.status = err.response?.status
        e.isNetworkError = !err.response
        throw e
      }
      throw err
    }
  }

  // ── Local session ─────────────────────────────────────────────────────
  setAuth(token: string, user: User): void {
    localStorage.setItem(TOKEN_KEY, token)
    localStorage.setItem(USER_KEY, JSON.stringify(user))
  }
  clearAuth(): void {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }
  isAuthenticated(): boolean {
    return !!localStorage.getItem(TOKEN_KEY)
  }
  getStoredUser(): User | null {
    try {
      const raw = localStorage.getItem(USER_KEY)
      return raw ? (JSON.parse(raw) as User) : null
    } catch {
      this.clearAuth()
      return null
    }
  }
}

export const apiClient = new APIClient()
export default apiClient
