import { describe, expect, it } from 'vitest'
import { compareTimestamps } from './timestamps'

describe('compareTimestamps', () => {
  it.each([
    ['2026-11-10T12:00:00Z', '2026-11-10T12:00:00.000001Z'],
    ['2026-11-10T12:00:00.12Z', '2026-11-10T12:00:00.120001Z'],
    ['2026-11-10T12:00:00.123456Z', '2026-11-10T20:00:00.123457+08:00'],
    ['2026-11-10T07:00:00.123456-05:00', '2026-11-10T12:00:00.123457Z'],
    ['2026-11-10T12:00:00.123456788Z', '2026-11-10T12:00:00.123456789Z'],
    ['1969-12-31T23:59:59.999999Z', '1970-01-01T00:00:00Z'],
  ])('compares %s before %s without losing precision', (earlier, later) => {
    expect(compareTimestamps(earlier, later)).toBe(-1)
    expect(compareTimestamps(later, earlier)).toBe(1)
  })

  it('recognizes equivalent instants with different fractional lengths and offsets', () => {
    expect(compareTimestamps('2026-11-10T12:00:00.12Z', '2026-11-10T20:00:00.120000+08:00')).toBe(0)
  })
})

