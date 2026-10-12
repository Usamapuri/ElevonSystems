/**
 * The store's host once served a different, Workbox-based app that registered
 * `/sw.js`. A browser that used it keeps that worker and its precache, and
 * shows the old app's cached "Not Found" shell instead of Elevon — forever,
 * because Elevon registers nothing that would replace it. `/sw.js` must
 * therefore always exist and self-destruct: unregister, drop every cache,
 * reload the open pages. It must also never be cached as an immutable asset.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = join(__dirname, '..', '..')

describe('legacy service worker on /sw.js', () => {
  const sw = readFileSync(join(root, 'public', 'sw.js'), 'utf8')

  it('unregisters itself and clears every cache', () => {
    expect(sw).toContain('registration.unregister()')
    expect(sw).toContain('caches.keys()')
    expect(sw).toContain('caches.delete(')
    expect(sw).toContain('skipWaiting()')
    expect(sw).toContain('clients.claim()')
  })

  it('reloads the pages it controls so they fetch the live app', () => {
    expect(sw).toMatch(/client\.navigate\(client\.url\)/)
  })

  it('is served by nginx without the one-year immutable cache header', () => {
    const nginx = readFileSync(join(root, 'nginx.conf.template'), 'utf8')
    const block = nginx.match(/location = \/sw\.js \{[^}]*\}/)
    expect(block, 'exact-match location for /sw.js').not.toBeNull()
    expect(block![0]).toContain('no-cache')
    expect(block![0]).not.toContain('immutable')
  })
})
