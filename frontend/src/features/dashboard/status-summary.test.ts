import { describe, expect, it } from 'vitest'
import { makeService } from '@/test/services'
import { deriveStatusSummary } from './status-summary'

describe('deriveStatusSummary', () => {
  it('derives counts and a rounded percentage from accepted services', () => {
    expect(deriveStatusSummary([makeService('one', 'up'), makeService('two', 'down'), makeService('three', 'unknown')])).toEqual({
      up: 1, down: 1, unknown: 1, total: 3, healthyPercentage: 33,
    })
  })

  it('has no percentage for an accepted empty collection', () => {
    expect(deriveStatusSummary([])).toEqual({ up: 0, down: 0, unknown: 0, total: 0, healthyPercentage: null })
  })

  it('supports additional services without a catalogue limit', () => {
    expect(deriveStatusSummary(Array.from({ length: 123 }, (_, i) => makeService(`new-${i}`, 'up')))).toMatchObject({ total: 123, up: 123, healthyPercentage: 100 })
  })

  it('includes observed and unobserved Unknown services without modifying timestamps', () => {
    const services = Object.freeze([
      Object.freeze(makeService('up', 'up', '2026-09-24T12:00:00.123456788Z')),
      Object.freeze(makeService('unobserved', 'unknown')),
      Object.freeze(makeService('observed', 'unknown', '2026-09-24T20:00:00.123456789+08:00')),
    ])
    const before = JSON.stringify(services)
    expect(deriveStatusSummary(services)).toMatchObject({ up: 1, unknown: 2 })
    expect(JSON.stringify(services)).toBe(before)
  })
})
