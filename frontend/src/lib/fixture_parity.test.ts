import { createHash } from 'crypto'
import { readFileSync } from 'fs'
import path from 'path'
import { describe, expect, it } from 'vitest'

// Fails the build if the Go and TS suites ever load different bytes: a
// change to one fixture without the other must not slip through. Mirrors
// backend/internal/pricing/fixture_parity_test.go.
describe('pricing fixture parity', () => {
  it('is byte-identical to the Go fixture', () => {
    const tsPath = path.resolve(__dirname, './pricing_fixture.json')
    const goPath = path.resolve(__dirname, '../../../backend/internal/pricing/testdata/pricing_fixture.json')

    const tsBytes = readFileSync(tsPath)
    const goBytes = readFileSync(goPath)

    const tsSum = createHash('sha256').update(tsBytes).digest('hex')
    const goSum = createHash('sha256').update(goBytes).digest('hex')

    expect(tsSum, `pricing fixture drift: ${tsPath} (${tsSum}) != ${goPath} (${goSum})`).toBe(goSum)
  })
})
