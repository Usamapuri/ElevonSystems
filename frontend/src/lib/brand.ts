/**
 * The store's own identity in the app chrome: its business name and the
 * logo uploaded under Settings → Business (the same image receipts print).
 * An accountant running several stores tells the browser tabs apart by the
 * document title, which leads with the store name.
 *
 * The sign-in page has no API access (every route but the three auth routes
 * is behind sign-in), so it shows what this browser last saw for this
 * origin, remembered in localStorage after each settings load. Nothing here
 * ever throws on a blocked or empty storage — the default brand is the
 * fallback everywhere.
 */

export const DEFAULT_BRAND_NAME = 'Elevon POS'
export const BRAND_STORAGE_KEY = 'elevon_brand'

export interface Brand {
  name: string
  /** '' when no logo is uploaded; otherwise an https:// URL or a data:image/ URL. */
  logo: string
}

export const DEFAULT_BRAND: Brand = { name: DEFAULT_BRAND_NAME, logo: '' }

interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

function safeLogo(raw: unknown): string {
  if (typeof raw !== 'string') return ''
  return raw.startsWith('data:image/') || raw.startsWith('https://') ? raw : ''
}

export function brandFromSettings(s: { business_name?: string; receipt_logo_url?: string } | null | undefined): Brand {
  const name = (s?.business_name ?? '').trim()
  return { name: name || DEFAULT_BRAND_NAME, logo: safeLogo(s?.receipt_logo_url) }
}

/** `Unity Exploration · Till`; the store first so tabs sort by store. */
export function documentTitle(page: string | null | undefined, brand: Brand | null | undefined): string {
  const name = brand?.name?.trim() || DEFAULT_BRAND_NAME
  const p = (page ?? '').trim()
  return p ? `${name} · ${p}` : name
}

export function rememberBrand(storage: StorageLike | null | undefined, brand: Brand): void {
  try {
    storage?.setItem(BRAND_STORAGE_KEY, JSON.stringify({ name: brand.name, logo: brand.logo }))
  } catch {
    // Private window, blocked storage: the sign-in page simply shows the default.
  }
}

export function recallBrand(storage: StorageLike | null | undefined): Brand {
  try {
    const raw = storage?.getItem(BRAND_STORAGE_KEY)
    if (!raw) return DEFAULT_BRAND
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return DEFAULT_BRAND
    const o = parsed as { name?: unknown; logo?: unknown }
    const name = typeof o.name === 'string' && o.name.trim() ? o.name.trim() : DEFAULT_BRAND_NAME
    return { name, logo: safeLogo(o.logo) }
  } catch {
    return DEFAULT_BRAND
  }
}

/** window.localStorage, or null where touching it throws. */
export function browserStorage(): StorageLike | null {
  try {
    return typeof window !== 'undefined' ? window.localStorage : null
  } catch {
    return null
  }
}
