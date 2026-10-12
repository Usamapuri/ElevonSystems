import { describe, expect, it } from 'vitest'
import { BRAND_STORAGE_KEY, DEFAULT_BRAND_NAME, brandFromSettings, documentTitle, recallBrand, rememberBrand } from './brand'

// The app chrome wears the store's own name and logo, so an accountant with
// several stores open can tell the tabs apart. The sign-in page, which has
// no API access, shows what this browser last saw for this origin.

class FakeStorage {
  map = new Map<string, string>()
  getItem(k: string) {
    return this.map.get(k) ?? null
  }
  setItem(k: string, v: string) {
    this.map.set(k, v)
  }
  removeItem(k: string) {
    this.map.delete(k)
  }
}

class BrokenStorage {
  getItem(): string | null {
    throw new Error('blocked')
  }
  setItem(): void {
    throw new Error('blocked')
  }
  removeItem(): void {
    throw new Error('blocked')
  }
}

describe('brandFromSettings', () => {
  it('uses the business name and the uploaded logo', () => {
    expect(brandFromSettings({ business_name: 'Unity Exploration', receipt_logo_url: 'data:image/png;base64,AAAA' })).toEqual({
      name: 'Unity Exploration',
      logo: 'data:image/png;base64,AAAA',
    })
  })
  it('falls back to Elevon POS while the name is blank, and to no logo', () => {
    expect(brandFromSettings({ business_name: '   ', receipt_logo_url: '' })).toEqual({ name: DEFAULT_BRAND_NAME, logo: '' })
    expect(brandFromSettings(undefined)).toEqual({ name: DEFAULT_BRAND_NAME, logo: '' })
  })
})

describe('documentTitle', () => {
  it('puts the store first so browser tabs are told apart by store', () => {
    expect(documentTitle('Till', { name: 'Unity Exploration', logo: '' })).toBe('Unity Exploration · Till')
  })
  it('is just the store name without a page', () => {
    expect(documentTitle('', { name: 'Unity Exploration', logo: '' })).toBe('Unity Exploration')
    expect(documentTitle(undefined, null)).toBe(DEFAULT_BRAND_NAME)
  })
})

describe('brand memory for the sign-in page', () => {
  it('round-trips through storage', () => {
    const s = new FakeStorage()
    rememberBrand(s, { name: 'Unity Exploration', logo: 'data:image/png;base64,AAAA' })
    expect(s.getItem(BRAND_STORAGE_KEY)).toBeTruthy()
    expect(recallBrand(s)).toEqual({ name: 'Unity Exploration', logo: 'data:image/png;base64,AAAA' })
  })
  it('recalls the default when nothing was stored, or storage holds junk', () => {
    expect(recallBrand(new FakeStorage())).toEqual({ name: DEFAULT_BRAND_NAME, logo: '' })
    const s = new FakeStorage()
    s.setItem(BRAND_STORAGE_KEY, '{not json')
    expect(recallBrand(s)).toEqual({ name: DEFAULT_BRAND_NAME, logo: '' })
    s.setItem(BRAND_STORAGE_KEY, JSON.stringify({ name: 'X', logo: 'javascript:alert(1)' }))
    expect(recallBrand(s)).toEqual({ name: 'X', logo: '' })
  })
  it('never throws when storage is unavailable', () => {
    const broken = new BrokenStorage()
    expect(() => rememberBrand(broken, { name: 'X', logo: '' })).not.toThrow()
    expect(recallBrand(broken)).toEqual({ name: DEFAULT_BRAND_NAME, logo: '' })
    expect(recallBrand(null)).toEqual({ name: DEFAULT_BRAND_NAME, logo: '' })
  })
})
