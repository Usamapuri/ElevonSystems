import { describe, expect, it } from 'vitest'
import { resolveApiBaseUrl } from './client'

describe('resolveApiBaseUrl', () => {
  it('defaults to localhost /api/v1', () => {
    expect(resolveApiBaseUrl(undefined)).toBe('http://localhost:8080/api/v1')
  })
  it('appends /api/v1 when missing', () => {
    expect(resolveApiBaseUrl('https://pos.example.com/')).toBe('https://pos.example.com/api/v1')
  })
  it('keeps a relative /api/v1 (nginx-proxied production build)', () => {
    expect(resolveApiBaseUrl('/api/v1')).toBe('/api/v1')
  })
})
