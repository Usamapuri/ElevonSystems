import { BarChart3, BookUser, CalendarCheck, FileText, LayoutDashboard, Scale, Settings, ShoppingCart } from 'lucide-react'

/** Mirrors backend/internal/util/roles.go and the users_role_check constraint. */
export const ROLES = ['admin', 'counter'] as const
export type Role = (typeof ROLES)[number]

export function isRole(value: string): value is Role {
  return (ROLES as readonly string[]).includes(value)
}

export interface NavItem {
  id: string
  label: string
  to: string
  icon: typeof ShoppingCart
  /** Roles that may open it. */
  roles: readonly Role[]
}

/** Sidebar order. `to` is the URL prefix used for access checks. */
export const NAV_ITEMS: readonly NavItem[] = [
  { id: 'pos', label: 'Till', to: '/pos', icon: ShoppingCart, roles: ['admin', 'counter'] },
  { id: 'day-close', label: 'Day close', to: '/day-close', icon: CalendarCheck, roles: ['admin', 'counter'] },
  { id: 'customers', label: 'Customers', to: '/customers', icon: BookUser, roles: ['admin', 'counter'] },
  { id: 'invoices', label: 'Invoices', to: '/invoices', icon: FileText, roles: ['admin', 'counter'] },
  { id: 'dashboard', label: 'Dashboard', to: '/dashboard', icon: LayoutDashboard, roles: ['admin'] },
  { id: 'reports', label: 'Reports', to: '/reports', icon: BarChart3, roles: ['admin'] },
  { id: 'rates', label: 'Rates', to: '/rates', icon: Scale, roles: ['admin'] },
  { id: 'settings', label: 'Settings', to: '/settings', icon: Settings, roles: ['admin'] },
]

/** Whether the role may open the URL (exact or prefix match on a nav item). */
export function canAccess(role: string, pathname: string): boolean {
  if (!isRole(role)) return false
  return NAV_ITEMS.some(
    (item) => item.roles.includes(role) && (pathname === item.to || pathname.startsWith(`${item.to}/`)),
  )
}

/** First screen after login. */
export function defaultPath(role: string): string {
  return role === 'admin' ? '/dashboard' : '/pos'
}

export function roleLabel(role: string): string {
  switch (role) {
    case 'admin':
      return 'Admin'
    case 'counter':
      return 'Counter'
    default:
      return role
  }
}
