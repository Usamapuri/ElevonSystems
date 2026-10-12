import { describe, expect, it } from 'vitest'
import { fbrBlockHtml } from './fbrBlock'

const qr = 'data:image/png;base64,iVBORw0KGgo='

describe('fbrBlockHtml (spec 2026-10-12 §2.6)', () => {
  it('prints the number and the QR on an individually filed sale', () => {
    const html = fbrBlockHtml({ fiscal_status: 'synced', fiscal_invoice_number: '6110180871403DIAJGEJ4031308' }, qr)
    expect(html).toContain('FBR Invoice No.')
    expect(html).toContain('6110180871403DIAJGEJ4031308')
    expect(html).toContain('<img src="data:image/png')
  })
  it('carries the FBR Digital Invoicing logo, embedded so the print window needs no network', () => {
    const html = fbrBlockHtml({ fiscal_status: 'synced', fiscal_invoice_number: '6110180871403DIAJGEJ4031308' }, qr)
    expect(html).toMatch(/<img src="data:image\/png;base64,[A-Za-z0-9+/=]+" alt="FBR Digital Invoicing System" class="fbr-logo" \/>/)
    expect(html.indexOf('fbr-logo')).toBeLessThan(html.indexOf('FBR Invoice No.'))
  })
  it('prints the number alone when there is no QR', () => {
    const html = fbrBlockHtml({ fiscal_status: 'synced', fiscal_invoice_number: '6110180871403DIAJGEJ4031308' }, null)
    expect(html).toContain('6110180871403DIAJGEJ4031308')
    expect(html).not.toContain('fbr-qr')
    expect(html).toContain('fbr-logo')
  })
  it('is empty for every other state — a covered sale’s number belongs to the combined invoice', () => {
    for (const status of ['covered', 'unfiled', 'pending', 'failed', 'off']) {
      expect(fbrBlockHtml({ fiscal_status: status, fiscal_invoice_number: '6110180871403DIAJGEJ4031308' }, qr)).toBe('')
    }
    expect(fbrBlockHtml({ fiscal_status: 'synced', fiscal_invoice_number: null }, qr)).toBe('')
  })
  it('escapes the number', () => {
    expect(fbrBlockHtml({ fiscal_status: 'synced', fiscal_invoice_number: '<b>x' }, null)).not.toContain('<b>')
  })
})
