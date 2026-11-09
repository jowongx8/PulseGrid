import type { ApiTimestamp, ServiceDTO } from '@/data/contracts'

// Go emits RFC3339Nano. Parse whole seconds separately so neither Date.parse's
// millisecond precision nor differing UTC offsets decide submillisecond ties.
function timestampNanoseconds(timestamp: ApiTimestamp): bigint {
  const fraction = /\.(\d{1,9})(?=Z|[+-]\d{2}:\d{2}$)/.exec(timestamp)
  const wholeSecond = fraction ? timestamp.replace(fraction[0], '') : timestamp
  return BigInt(Date.parse(wholeSecond)) * 1_000_000n
    + BigInt((fraction?.[1] ?? '').padEnd(9, '0'))
}

export function deriveStatusSummary(services: readonly ServiceDTO[]) {
  const counts = { up: 0, down: 0, unknown: 0 }
  let latestCheckedAt: ApiTimestamp | null = null
  let latestNanoseconds: bigint | null = null

  for (const service of services) {
    counts[service.status.state] += 1
    const timestamp = service.status.lastCheckedAt
    if (timestamp === null) continue

    const nanoseconds = timestampNanoseconds(timestamp)
    if (latestNanoseconds === null || nanoseconds > latestNanoseconds) {
      latestNanoseconds = nanoseconds
      latestCheckedAt = timestamp
    }
  }

  return {
    ...counts,
    total: services.length,
    healthyPercentage: services.length === 0 ? null : Math.round(counts.up / services.length * 100),
    latestCheckedAt,
  }
}

export function formatServiceCheck(timestamp: ApiTimestamp): string {
  // Display whole seconds in the viewer's locale/timezone; keep the API string
  // untouched for selection and the <time> element's dateTime attribute.
  const wholeSecond = timestamp.replace(/\.\d{1,9}(?=Z|[+-]\d{2}:\d{2}$)/, '')
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'shortOffset',
  }).format(Date.parse(wholeSecond))
}
