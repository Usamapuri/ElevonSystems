import { describe, expect, it } from 'vitest'
import { canAccess, defaultPath, isRole, NAV_ITEMS, roleLabel, visibleNavItems } from './roles'

describe('roles', () => {
  it('knows exactly admin and counter', () => {
    expect(isRole('admin')).toBe(true)
    expect(isRole('counter')).toBe(true)
    expect(isRole('manager')).toBe(false)
  })
  it('admin can open everything', () => {
    for (const item of NAV_ITEMS) expect(canAccess('admin', item.to)).toBe(true)
  })
  it('counter opens till, day close, customers, invoices only', () => {
    expect(canAccess('counter', '/pos')).toBe(true)
    expect(canAccess('counter', '/day-close')).toBe(true)
    expect(canAccess('counter', '/customers/abc')).toBe(true)
    expect(canAccess('counter', '/invoices')).toBe(true)
    expect(canAccess('counter', '/reports')).toBe(false)
    expect(canAccess('counter', '/settings')).toBe(false)
    expect(canAccess('counter', '/rates')).toBe(false)
    expect(canAccess('counter', '/dashboard')).toBe(false)
    expect(canAccess('counter', '/fbr')).toBe(false)
    expect(canAccess('admin', '/fbr')).toBe(true)
  })
  it('lands admin on dashboard and counter on the till', () => {
    expect(defaultPath('admin')).toBe('/dashboard')
    expect(defaultPath('counter')).toBe('/pos')
  })
  it('labels roles', () => {
    expect(roleLabel('counter')).toBe('Counter')
  })
})

describe('visibleNavItems', () => {
  it('drops Day close from the rail when the switch is off, keeps it on', () => {
    const off = visibleNavItems('counter', { day_close_enabled: false }).map((i) => i.id)
    const on = visibleNavItems('counter', { day_close_enabled: true }).map((i) => i.id)
    expect(off).not.toContain('day-close')
    expect(on).toContain('day-close')
    expect(off).toContain('pos')
  })
  it('shows everything while settings are still loading', () => {
    expect(visibleNavItems('admin', undefined).map((i) => i.id)).toContain('day-close')
  })
})
