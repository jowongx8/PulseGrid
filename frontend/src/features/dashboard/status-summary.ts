import type { ServiceDTO } from '@/data/contracts'

export function deriveStatusSummary(services: readonly ServiceDTO[]) {
  const counts = { up: 0, down: 0, unknown: 0 }

  for (const service of services) {
    counts[service.status.state] += 1
  }

  return {
    ...counts,
    total: services.length,
    healthyPercentage: services.length === 0 ? null : Math.round(counts.up / services.length * 100),
  }
}
