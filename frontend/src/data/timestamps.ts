import type { ApiTimestamp } from './contracts'

// Parse whole seconds separately; retain up to Go's nanosecond precision and
// timezone offsets without modifying or reserializing the original API value.
export function timestampNanoseconds(timestamp: ApiTimestamp): bigint {
  const fraction = /\.(\d{1,9})(?=Z|[+-]\d{2}:\d{2}$)/.exec(timestamp)
  const wholeSecond = fraction ? timestamp.replace(fraction[0], '') : timestamp
  return BigInt(Date.parse(wholeSecond)) * 1_000_000n
    + BigInt((fraction?.[1] ?? '').padEnd(9, '0'))
}

export function compareTimestamps(left: ApiTimestamp, right: ApiTimestamp): number {
  const a = timestampNanoseconds(left)
  const b = timestampNanoseconds(right)
  return a < b ? -1 : a > b ? 1 : 0
}
