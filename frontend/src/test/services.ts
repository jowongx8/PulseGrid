import type { ApiTimestamp, ServiceDTO, ServiceState } from '@/data/contracts'

export function makeService(
  id: string,
  state: ServiceState,
  lastCheckedAt: ApiTimestamp | null = null,
): ServiceDTO {
  return {
    id,
    name: `Service ${id}`,
    category: 'ai',
    websiteUrl: 'https://example.com',
    weight: 1,
    status: { state, lastCheckedAt },
  }
}
