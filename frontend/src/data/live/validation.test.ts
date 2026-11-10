import { describe, expect, it } from 'vitest'
import type { ServiceCategory } from '../contracts'
import { incidentsResponseFixture, statusResponseFixture } from '../fixtures'
import { validateIncidentsResponse, validateStatusResponse } from './validation'

const service = statusResponseFixture.services[0]
const active = incidentsResponseFixture.active[0]
const resolved = incidentsResponseFixture.resolved[0]
const withService = (changes: Record<string, unknown>) => ({ services: [{ ...service, ...changes }] })
const withStatus = (changes: Record<string, unknown>) => withService({ status: { ...service.status, ...changes } })
const withActive = (changes: Record<string, unknown>) => ({ active: [{ ...active, ...changes }], resolved: [] })
const withResolved = (changes: Record<string, unknown>) => ({ active: [], resolved: [{ ...resolved, ...changes }] })

describe('status response validation', () => {
  it('accepts the current fixtures and empty service arrays', () => {
    expect(() => validateStatusResponse(statusResponseFixture)).not.toThrow()
    expect(() => validateStatusResponse({ services: [] })).not.toThrow()
  })

  it.each([
    'developer-cloud', 'ai', 'social-media', 'communication',
    'productivity', 'entertainment', 'gaming', 'commerce',
  ] satisfies ServiceCategory[])('accepts the supported %s category', (category) => {
    expect(() => validateStatusResponse(withService({ category }))).not.toThrow()
  })

  it.each(['future-category', 'AI', '__proto__', 'toString'])('rejects unsupported category %s', (category) => {
    expect(() => validateStatusResponse(withService({ category }))).toThrow('services[0].category')
  })

  it.each(['up', 'down', 'unknown'])('accepts %s with nullable or observed lastCheckedAt', (state) => {
    expect(() => validateStatusResponse(withStatus({ state, lastCheckedAt: null }))).not.toThrow()
    expect(() => validateStatusResponse(withStatus({ state, lastCheckedAt: '2026-09-21T14:51:58.123456789+10:00' }))).not.toThrow()
  })

  it.each([
    [null, 'response'], [[], 'response'], [true, 'response'], [{}, 'services'],
    [{ services: null }, 'services'], [{ services: {} }, 'services'], [{ services: [null] }, 'services[0]'],
    [withService({ id: undefined }), 'id'], [withService({ id: '' }), 'id'],
    [withService({ name: 123 }), 'name'], [withService({ name: ' ' }), 'name'],
    [withService({ websiteUrl: null }), 'websiteUrl'], [withService({ category: 1 }), 'category'],
    [withService({ status: undefined }), 'status'], [withService({ status: [] }), 'status'],
    [withStatus({ state: 'degraded' }), 'state'], [withStatus({ state: 'UP' }), 'state'],
    [withStatus({ lastCheckedAt: undefined }), 'lastCheckedAt'], [withStatus({ lastCheckedAt: 0 }), 'lastCheckedAt'],
  ])('rejects incompatible payload %# at %s', (payload, path) => {
    expect(() => validateStatusResponse(payload)).toThrow(TypeError)
    expect(() => validateStatusResponse(payload)).toThrow(path)
  })

  it.each([0, -1, 1.5, Infinity, NaN, Number.MAX_SAFE_INTEGER + 1, '10', null])('rejects invalid weight %s', (weight) => {
    expect(() => validateStatusResponse(withService({ weight }))).toThrow('services[0].weight')
  })

  it('accepts new service IDs and preserves additive properties and catalogue order', () => {
    const body = Object.freeze({
      services: Object.freeze([
        Object.freeze({ ...service, id: 'new-service', name: ' New service ', futureField: true }),
        Object.freeze({ ...service, id: 'another-new-service' }),
      ]),
      futureProperty: 'retained',
    })
    expect(() => validateStatusResponse(body)).not.toThrow()
    expect(body.services.map((item) => item.id)).toEqual(['new-service', 'another-new-service'])
    expect(body.services[0].name).toBe(' New service ')
    expect(body.services[0]).toHaveProperty('futureField', true)
    expect(body.futureProperty).toBe('retained')
  })
})

describe('timestamp validation', () => {
  it.each(Array.from({ length: 10 }, (_, digits) => digits))('accepts and preserves %i fractional digits', (digits) => {
    const timestamp = `2026-09-21T14:51:58${digits ? `.${'123456789'.slice(0, digits)}` : ''}+10:00`
    const body = withStatus({ lastCheckedAt: timestamp })
    validateStatusResponse(body)
    expect(body.services[0].status.lastCheckedAt).toBe(timestamp)
  })

  it.each([
    '2000-02-29T12:00:00Z', '2024-02-29T23:59:59.1Z',
    '2026-01-01T00:00:00.000000001-05:00', '2026-09-24T12:00:00.120000+08:00',
  ])('accepts valid Go timestamps: %s', (timestamp) => {
    expect(() => validateStatusResponse(withStatus({ lastCheckedAt: timestamp }))).not.toThrow()
  })

  it.each([
    '', 'invalid', '2026-09-21', '2026-09-21T14:51:58', '2026-09-21T14:51:58.Z',
    '2026-09-21T14:51:58.1234567890Z', '2026-09-21T14:51:58Z\n', '2026-09-21T14:51:58Z ',
    '2026-09-21T14:51:58+24:00', '2026-09-21T14:51:58+10:60',
    '2026-09-21T24:00:00Z', '2026-09-21T14:60:00Z', '2026-09-21T14:51:60Z',
    '2026-00-21T14:51:58Z', '2026-13-21T14:51:58Z', '2026-09-00T14:51:58Z',
    '2026-02-29T14:51:58Z', '1900-02-29T14:51:58Z', '2026-04-31T14:51:58Z',
  ])('rejects malformed timestamps without Date normalization: %j', (timestamp) => {
    expect(() => validateStatusResponse(withStatus({ lastCheckedAt: timestamp }))).toThrow('lastCheckedAt')
  })
})

describe('incident response validation', () => {
  it('accepts fixtures, empty groups and incidents independent of the current service catalogue', () => {
    expect(() => validateIncidentsResponse(incidentsResponseFixture)).not.toThrow()
    expect(() => validateIncidentsResponse({ active: [], resolved: [] })).not.toThrow()
    expect(() => validateIncidentsResponse(withActive({ serviceId: 'previously-monitored-service' }))).not.toThrow()
  })

  it.each([
    [null, 'response'], [[], 'response'], [{}, 'active'], [{ active: [] }, 'resolved'],
    [{ active: null, resolved: [] }, 'active'], [{ active: [], resolved: null }, 'resolved'],
    [{ active: [null], resolved: [] }, 'active[0]'], [{ active: [], resolved: ['invalid'] }, 'resolved[0]'],
    [withActive({ serviceId: undefined }), 'serviceId'], [withActive({ serviceId: '' }), 'serviceId'],
    [withActive({ startedAt: null }), 'startedAt'], [withActive({ startedAt: 123 }), 'startedAt'],
    [withActive({ resolvedAt: undefined }), 'resolvedAt'], [withActive({ resolvedAt: resolved.resolvedAt }), 'resolvedAt'],
    [withResolved({ resolvedAt: undefined }), 'resolvedAt'], [withResolved({ resolvedAt: null }), 'resolvedAt'],
    [withResolved({ resolvedAt: 'invalid' }), 'resolvedAt'],
  ])('rejects malformed incident payload %# at %s', (payload, path) => {
    expect(() => validateIncidentsResponse(payload)).toThrow(TypeError)
    expect(() => validateIncidentsResponse(payload)).toThrow(path)
  })

  it('compares resolution chronology precisely across offsets and fractional lengths', () => {
    const startedAt = '2026-09-24T12:00:00.12Z'
    const body = withResolved({ startedAt, resolvedAt: '2026-09-24T20:00:00.120000001+08:00' })
    expect(() => validateIncidentsResponse(body)).not.toThrow()
    expect(body.resolved[0].startedAt).toBe(startedAt)
    for (const resolvedAt of ['2026-09-24T20:00:00.120000+08:00', '2026-09-24T07:00:00.119999999-05:00']) {
      expect(() => validateIncidentsResponse(withResolved({ startedAt, resolvedAt }))).toThrow('resolved[0].resolvedAt')
    }
  })

  it('preserves raw incident identity strings, feed order and all active incidents', () => {
    const entries = Array.from({ length: 25 }, (_, index) => Object.freeze({
      serviceId: `service-${index}`,
      startedAt: `2026-09-24T12:00:00.${String(25 - index).padStart(6, '0')}Z`,
      resolvedAt: null,
    }))
    const body = Object.freeze({ active: Object.freeze(entries), resolved: Object.freeze([]) })
    const identities = entries.map((incident) => `${incident.serviceId}|${incident.startedAt}`)
    validateIncidentsResponse(body)
    expect(body.active.map((incident) => `${incident.serviceId}|${incident.startedAt}`)).toEqual(identities)
    expect(body.active).toHaveLength(25)
  })
})
