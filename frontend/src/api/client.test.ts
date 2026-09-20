import { describe, expect, it } from 'vitest'
import { errorCodeFromResponse, isSessionExpiryCode, resolveApiBaseUrl } from './client'

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

describe('errorCodeFromResponse', () => {
  it('reads the code straight off an already-parsed JSON body', async () => {
    await expect(
      errorCodeFromResponse({ data: { success: false, message: 'Session expired', error: 'token_revoked' } }),
    ).resolves.toBe('token_revoked')
  })

  it('unwraps a JSON Blob body — the shape a responseType: "blob" request gets back', async () => {
    const blob = new Blob([JSON.stringify({ success: false, message: 'Session expired', error: 'auth_required' })], {
      type: 'application/json; charset=utf-8',
    })
    await expect(errorCodeFromResponse({ data: blob })).resolves.toBe('auth_required')
  })

  it('is undefined for a non-JSON Blob body (an HTML proxy error page, say)', async () => {
    const blob = new Blob(['<html>502 Bad Gateway</html>'], { type: 'text/html' })
    await expect(errorCodeFromResponse({ data: blob })).resolves.toBeUndefined()
  })

  it('is undefined when there is no response at all (a network error)', async () => {
    await expect(errorCodeFromResponse(undefined)).resolves.toBeUndefined()
  })
})
