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
