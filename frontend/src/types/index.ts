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
