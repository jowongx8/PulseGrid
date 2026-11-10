import type { ApiTimestamp, ServiceDTO } from '@/data/contracts'

import { timestampNanoseconds } from './timestamps'

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
