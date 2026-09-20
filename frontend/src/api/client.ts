import axios, { type AxiosInstance, type AxiosRequestConfig } from 'axios'
import type {
  APIResponse, AppSettings, BusinessDay, CashMovement, CashMovementRequest, CloseDayRequest, CreateCustomerRequest,
  CreateInvoiceRequest, CreateProductRequest, CreateUserRequest, Customer, CustomerListParams, DayCurrent,
  DayHistoryParams, ForceCloseDayRequest, Invoice, InvoiceListParams, LoginRequest, LoginResponse, OpenDayRequest,
  PaginatedResponse, Product, ProductListParams, RateHistoryEntry, RateHistoryParams, RecentInvoiceParams,
  ReopenDayRequest, SettingsPatch, StatementParams, StatementRow, UpdateCustomerRequest, UpdateProductRequest,
  UpdateRatesRequest, UpdateRatesResponse, UpdateUserRequest, User, UserListParams, VoidInvoiceRequest, ZReport,
} from '@/types'

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
      (error) => {
        if (error.response?.status === 401) {
          const code = (error.response?.data as APIResponse | undefined)?.error
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
