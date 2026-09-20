import { describe, expect, it } from 'vitest'
import { isSessionExpiryCode, resolveApiBaseUrl } from './client'

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

describe('isSessionExpiryCode', () => {
  it('is false for a mistyped PIN — the cart must survive it', () => {
    expect(isSessionExpiryCode('invalid_pin')).toBe(false)
  })
  it('is false for a stripped proxy header — not an expired session', () => {
    expect(isSessionExpiryCode('missing_auth_header')).toBe(false)
  })
  it('is false for a bad login attempt', () => {
    expect(isSessionExpiryCode('invalid_credentials')).toBe(false)
  })
  it('is false for undefined', () => {
    expect(isSessionExpiryCode(undefined)).toBe(false)
  })
  it.each([
    'invalid_auth_format',
    'invalid_token',
    'token_revoked',
    'user_inactive',
    'auth_check_failed',
    'auth_required',
  ])('is true for the session-expiry code %s', (code) => {
    expect(isSessionExpiryCode(code)).toBe(true)
  })
})
