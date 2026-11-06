import { describe, expect, it } from 'vitest'
import type { IncidentsResponse, StatusResponse } from '@/data/contracts'
import { incidentsResponseFixture, statusResponseFixture } from '@/data/fixtures'

const { services }: StatusResponse = statusResponseFixture
const incidents: IncidentsResponse = incidentsResponseFixture
const allIncidents = [...incidents.active, ...incidents.resolved]
const microsecondUTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/

describe('development fixtures', () => {
  it('contains services with unique IDs', () => {
    const ids = services.map((service) => service.id)
    expect(ids.length).toBeGreaterThan(0)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('uses positive weights', () => {
    for (const service of services) expect(service.weight).toBeGreaterThan(0)
  })

  it('uses supported service states', () => {
    for (const service of services) {
      expect(['up', 'down', 'unknown']).toContain(service.status.state)
    }
  })

  it('covers both unobserved and observed Unknown services', () => {
    const unknown = services.filter((service) => service.status.state === 'unknown')
    expect(unknown.some((service) => service.status.lastCheckedAt === null)).toBe(true)
    expect(unknown.some((service) => service.status.lastCheckedAt !== null)).toBe(true)
  })

  it('references existing fixture services from incidents', () => {
    const ids = new Set(services.map((service) => service.id))
    for (const incident of allIncidents) expect(ids.has(incident.serviceId)).toBe(true)
  })

  it('keeps active incidents unresolved and resolved incidents resolved', () => {
    for (const incident of incidents.active) expect(incident.resolvedAt).toBeNull()
    for (const incident of incidents.resolved) expect(incident.resolvedAt).not.toBeNull()
  })

  it('has unique identities using unchanged incident start strings', () => {
    const keys = allIncidents.map((incident) => `${incident.serviceId}|${incident.startedAt}`)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('preserves UTC microseconds, valid chronology, and newest-first feed ordering', () => {
    const starts = incidents.active.map((incident) => {
      expect(incident.startedAt).toMatch(microsecondUTC)
      return incident.startedAt
    })
    const resolutions = incidents.resolved.map((incident) => {
      expect(incident.startedAt).toMatch(microsecondUTC)
      const resolvedAt = incident.resolvedAt
      if (resolvedAt === null) throw new Error('Resolved fixture has no resolution timestamp')
      expect(resolvedAt).toMatch(microsecondUTC)
      // Uniform UTC microsecond strings sort chronologically without losing precision.
      expect(resolvedAt > incident.startedAt).toBe(true)
      return resolvedAt
    })

    expect(starts).toEqual([...starts].sort().reverse())
    expect(resolutions).toEqual([...resolutions].sort().reverse())
  })
})
