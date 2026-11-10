import { describe, expect, it } from 'vitest'
import { makeService } from '@/test/services'
import { deriveStatusSummary } from './status-summary'

describe('deriveStatusSummary', () => {
  it('derives counts and a rounded percentage from any service collection', () => {
    const services = [
      makeService('one', 'up'),
      makeService('two', 'down'),
      makeService('three', 'unknown'),
    ]

    expect(deriveStatusSummary(services)).toEqual({
      up: 1, down: 1, unknown: 1, total: 3, healthyPercentage: 33, latestCheckedAt: null,
    })
  })

  it('has no percentage or check time for an empty collection', () => {
    expect(deriveStatusSummary([])).toEqual({
      up: 0, down: 0, unknown: 0, total: 0, healthyPercentage: null, latestCheckedAt: null,
    })
  })

  it('supports additional service IDs and counts without a catalogue limit', () => {
    const services = Array.from({ length: 123 }, (_, i) => makeService(`new-${i}`, 'up'))
    expect(deriveStatusSummary(services)).toMatchObject({ total: 123, up: 123, healthyPercentage: 100 })
  })

  it('includes observed Unknown services and preserves inputs', () => {
    const latest = '2026-09-24T20:00:00.123456789+08:00'
    const services = Object.freeze([
      Object.freeze(makeService('up', 'up', '2026-09-24T12:00:00.123456788Z')),
      Object.freeze(makeService('unobserved', 'unknown')),
      Object.freeze(makeService('observed', 'unknown', latest)),
    ])
    const before = JSON.stringify(services)

    expect(deriveStatusSummary(services).latestCheckedAt).toBe(latest)
    expect(JSON.stringify(services)).toBe(before)
  })

  it.each([
    ['2026-09-24T12:10:00Z', '2026-09-24T20:00:00+08:00', '2026-09-24T12:10:00Z'],
    ['2026-09-24T12:00:00.123456788Z', '2026-09-24T20:00:00.123456789+08:00', '2026-09-24T20:00:00.123456789+08:00'],
    ['2026-09-24T07:00:00.123456789-05:00', '2026-09-24T12:00:00.123456788Z', '2026-09-24T07:00:00.123456789-05:00'],
    ['2026-09-24T12:00:00.1234Z', '2026-09-24T12:00:00.123400001Z', '2026-09-24T12:00:00.123400001Z'],
    ['2026-09-24T12:00:00.12Z', '2026-09-24T12:00:00.119999999Z', '2026-09-24T12:00:00.12Z'],
    ['2026-09-24T12:00:00Z', '2026-09-24T12:00:00.000000001Z', '2026-09-24T12:00:00.000000001Z'],
    ['2026-09-24T11:59:59.999999999Z', '2026-09-24T12:00:00Z', '2026-09-24T12:00:00Z'],
    ['1969-12-31T23:59:59.999999999Z', '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z'],
  ])('orders %s and %s as precise instants', (first, second, expected) => {
    const services = [makeService('a', 'up', first), makeService('b', 'up', second)]
    expect(deriveStatusSummary(services).latestCheckedAt).toBe(expected)
    expect(deriveStatusSummary([...services].reverse()).latestCheckedAt).toBe(expected)
  })

  it('retains the first raw string when instants are equal', () => {
    const first = '2026-09-24T20:00:00.123400+08:00'
    const services = [
      makeService('first', 'up', first),
      makeService('second', 'up', '2026-09-24T12:00:00.1234Z'),
    ]
    expect(deriveStatusSummary(services).latestCheckedAt).toBe(first)
  })
})
