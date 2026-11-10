import { describe, expect, it, vi } from 'vitest'
import { formatTimestamp } from './timestamps'

describe('formatTimestamp', () => {
  it.each([
    ['Asia/Kuala_Lumpur', /20:00:00/, /GMT\+8/],
    ['America/New_York', /08:00:00/, /GMT-4/],
  ])('formats local date/time and offset in %s', (timeZone, clock, offset) => {
    const RealDateTimeFormat = Intl.DateTimeFormat
    // Control the viewer's locale/timezone, leaving the actual formatter intact.
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(function (_locales, options) {
      return new RealDateTimeFormat('en-GB', { ...options, timeZone, hourCycle: 'h23' })
    })

    const formatted = formatTimestamp('2026-09-24T12:00:00.123456789Z')
    expect(formatted).toMatch(/24 Sept 2026/)
    expect(formatted).toMatch(clock)
    expect(formatted).toMatch(offset)
  })
})
