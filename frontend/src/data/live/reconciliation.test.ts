import { describe, expect, it } from 'vitest'
import type {
  IncidentOpenedEventPayload, IncidentResolvedEventPayload, IncidentsResponse, ServiceState,
} from '../contracts'
import { makeService } from '@/test/services'
import type { LiveEvent } from './events'
import {
  applyLiveEvent, createInitialReconciliationState, reconcileSnapshots,
  type ReconciliationState,
} from './reconciliation'

const t0 = '2026-11-11T00:00:00.123456Z'
const t1 = '2026-11-11T00:00:01.123456Z'
const t2 = '2026-11-11T00:00:02.123456Z'
const t3 = '2026-11-11T00:00:03.123456Z'
const t4 = '2026-11-11T00:00:04.123456Z'

function opening(serviceId = 'a', startedAt = t1): IncidentOpenedEventPayload {
  return { serviceId, startedAt, resolvedAt: null }
}

function resolution(serviceId = 'a', startedAt = t1, resolvedAt = t2): IncidentResolvedEventPayload {
  return { serviceId, startedAt, resolvedAt }
}

function statusEvent(serviceId = 'a', state: ServiceState = 'down', lastCheckedAt = t2): LiveEvent {
  return { type: 'status.changed', payload: { serviceId, state, lastCheckedAt } }
}

function openEvent(serviceId = 'a', startedAt = t1): LiveEvent {
  return { type: 'incident.opened', payload: opening(serviceId, startedAt) }
}

function resolveEvent(serviceId = 'a', startedAt = t1, resolvedAt = t2): LiveEvent {
  return { type: 'incident.resolved', payload: resolution(serviceId, startedAt, resolvedAt) }
}

function baseline(incidents: IncidentsResponse = { active: [], resolved: [] }): ReconciliationState {
  return reconcileSnapshots(createInitialReconciliationState(), {
    status: { services: [makeService('a', 'up', t1), makeService('b', 'unknown')] }, incidents,
  }).state
}

describe('resources and catalogue authority', () => {
  it('distinguishes missing baselines from accepted empty resources', () => {
    const initial = createInitialReconciliationState()
    expect(initial.services).toBeNull()
    expect(initial.incidents).toBeNull()
    expect(reconcileSnapshots(initial, {}).state).toBe(initial)
    const statusOnly = reconcileSnapshots(initial, { status: { services: [] } })
    expect(statusOnly.state.services).toEqual([])
    expect(statusOnly.state.incidents).toBeNull()
    const incidentsOnly = reconcileSnapshots(initial, { incidents: { active: [], resolved: [] } })
    expect(incidentsOnly.state.services).toBeNull()
    expect(incidentsOnly.state.incidents).toEqual({ active: [], resolved: [] })
    expect(incidentsOnly.successful).toBe(true)
  })

  it('replaces catalogue membership, metadata and order only through REST', () => {
    const previous = baseline()
    const changed = { ...makeService('b', 'up', t2), name: 'Renamed', category: 'gaming' as const, websiteUrl: 'https://new.example', weight: 90 }
    const added = makeService('c', 'unknown')
    const result = reconcileSnapshots(previous, { status: { services: [changed, added] } })
    expect(result.successful).toBe(true)
    expect(result.state.services).toEqual([changed, added])
    expect(result.state.services?.[0]).toBe(changed)
    expect(result.state.incidents).toBe(previous.incidents)
  })

  it('keeps newer observations while accepting changed REST metadata', () => {
    const previous = applyLiveEvent(baseline(), statusEvent('a', 'down', t3)).state
    const incoming = { ...makeService('a', 'up', t0), weight: 200, name: 'New name' }
    const result = reconcileSnapshots(previous, { status: { services: [incoming] } })
    expect(result.state.services?.[0]).toEqual({ ...incoming, status: previous.services?.[0].status })
    expect(result.state.services?.[0].status).toBe(previous.services?.[0].status)
    expect(result.successful).toBe(true)
  })

  it('rejects duplicate catalogue IDs without losing the accepted slice', () => {
    const previous = baseline()
    const result = reconcileSnapshots(previous, { status: { services: [makeService('a', 'up'), makeService('a', 'down')] } })
    expect(result.successful).toBe(false)
    expect(result.issues).toContainEqual(expect.objectContaining({ kind: 'duplicate-service', serviceId: 'a' }))
    expect(result.state).toBe(previous)
  })

  it('installs the new REST catalogue before replaying events for new services', () => {
    const result = reconcileSnapshots(baseline(), { status: { services: [makeService('new', 'unknown')] } }, [statusEvent('new')])
    expect(result.successful).toBe(true)
    expect(result.state.services?.[0].status).toEqual({ state: 'down', lastCheckedAt: t2 })
    expect(result.state.services?.[0].status).not.toHaveProperty('serviceId')
  })

  it('does not invent services from SSE and diagnoses missing status baselines', () => {
    const initial = createInitialReconciliationState()
    const missing = applyLiveEvent(initial, statusEvent())
    expect(missing.state).toBe(initial)
    expect(missing.issues[0].kind).toBe('missing-baseline')
    const previous = baseline()
    const unknown = applyLiveEvent(previous, statusEvent('other'))
    expect(unknown.state).toBe(previous)
    expect(unknown.issues[0]).toMatchObject({ kind: 'unknown-service', serviceId: 'other', resource: 'status' })
  })
})

describe('status observation ordering', () => {
  it.each([
    { acceptedAt: null, accepted: 'unknown', incomingAt: null, incoming: 'unknown', expected: 'unknown', success: true },
    { acceptedAt: null, accepted: 'unknown', incomingAt: null, incoming: 'up', expected: 'unknown', success: false },
    { acceptedAt: null, accepted: 'unknown', incomingAt: t0, incoming: 'down', expected: 'down', success: true },
    { acceptedAt: t1, accepted: 'up', incomingAt: null, incoming: 'unknown', expected: 'up', success: true },
    { acceptedAt: t1, accepted: 'up', incomingAt: t0, incoming: 'down', expected: 'up', success: true },
    { acceptedAt: t1, accepted: 'up', incomingAt: t1, incoming: 'up', expected: 'up', success: true },
    { acceptedAt: t1, accepted: 'up', incomingAt: t1, incoming: 'down', expected: 'up', success: false },
    { acceptedAt: t1, accepted: 'up', incomingAt: t2, incoming: 'down', expected: 'down', success: true },
  ] as const)('selects $expected for $acceptedAt / $incomingAt ($incoming)', ({ acceptedAt, accepted, incomingAt, incoming, expected, success }) => {
    const previous = reconcileSnapshots(createInitialReconciliationState(), { status: { services: [makeService('a', accepted, acceptedAt)] } }).state
    const result = reconcileSnapshots(previous, { status: { services: [makeService('a', incoming, incomingAt)] } })
    expect(result.successful).toBe(success)
    expect(result.state.services?.[0].status.state).toBe(expected)
    if (expected === accepted) expect(result.state).toBe(previous)
    if (!success) expect(result.issues[0].kind).toBe('status-conflict')
  })

  it('compares nanoseconds, variable fractional lengths and timezone offsets', () => {
    const acceptedAt = '2026-11-11T08:00:00.123456700+08:00'
    const previous = reconcileSnapshots(createInitialReconciliationState(), { status: { services: [makeService('a', 'up', acceptedAt)] } }).state
    const duplicate = applyLiveEvent(previous, statusEvent('a', 'up', '2026-11-10T19:00:00.1234567-05:00'))
    expect(duplicate.state).toBe(previous)
    expect(duplicate.state.services?.[0].status.lastCheckedAt).toBe(acceptedAt)
    const stale = applyLiveEvent(previous, statusEvent('a', 'down', '2026-11-11T00:00:00.123456699Z'))
    expect(stale.state).toBe(previous)
    const newerAt = '2026-11-10T19:00:00.123456701-05:00'
    const newer = applyLiveEvent(previous, statusEvent('a', 'down', newerAt))
    expect(newer.state.services?.[0].status).toEqual({ state: 'down', lastCheckedAt: newerAt })
  })

  it('reports equivalent-instant conflicts and retains the accepted raw timestamp', () => {
    const previous = baseline()
    const result = applyLiveEvent(previous, statusEvent('a', 'down', '2026-11-11T08:00:01.123456000+08:00'))
    expect(result.state).toBe(previous)
    expect(result.successful).toBe(false)
    expect(result.issues[0]).toMatchObject({ kind: 'status-conflict', serviceId: 'a' })
  })

  it('preserves the entire status slice on snapshot conflict while accepting incidents', () => {
    const previous = baseline()
    const result = reconcileSnapshots(previous, {
      status: { services: [makeService('a', 'down', t1), makeService('b', 'down', t2)] },
      incidents: { active: [opening()], resolved: [] },
    })
    expect(result.successful).toBe(false)
    expect(result.state.services).toBe(previous.services)
    expect(result.state.incidents?.active).toEqual([opening()])
  })

  it('replays in supplied order and keeps earlier conflicts visible after later valid events', () => {
    const result = reconcileSnapshots(baseline(), {}, [statusEvent('a', 'down', t1), statusEvent('a', 'down', t3), statusEvent('a', 'up', t2)])
    expect(result.successful).toBe(false)
    expect(result.issues).toHaveLength(1)
    expect(result.state.services?.[0].status).toEqual({ state: 'down', lastCheckedAt: t3 })
  })

  it('allows a fresh corroborating snapshot or newer observation after a conflict', () => {
    const conflict = applyLiveEvent(baseline(), statusEvent('a', 'down', t1))
    const corroborated = reconcileSnapshots(conflict.state, { status: { services: [makeService('a', 'up', t1)] } })
    expect(corroborated.successful).toBe(true)
    expect(corroborated.issues).toEqual([])
    expect(applyLiveEvent(conflict.state, statusEvent('a', 'down', t2)).successful).toBe(true)
  })

  it('updates only the affected service and preserves unrelated DTO references', () => {
    const previous = baseline()
    const result = applyLiveEvent(previous, statusEvent())
    expect(result.state.services).not.toBe(previous.services)
    expect(result.state.services?.[1]).toBe(previous.services?.[1])
    expect(result.state.services?.[0]).toEqual({ ...previous.services?.[0], status: { state: 'down', lastCheckedAt: t2 } })
    expect(result.state.incidents).toBe(previous.incidents)
    expect(result.state.lifecycle).toBe(previous.lifecycle)
  })
})

describe('incident lifecycle', () => {
  it('opens and resolves without coupling incidents to status', () => {
    const previous = baseline()
    const opened = applyLiveEvent(previous, openEvent('b')).state
    expect(opened.incidents?.active).toEqual([opening('b')])
    expect(opened.services).toBe(previous.services)
    const changed = applyLiveEvent(opened, statusEvent('b', 'up', t3)).state
    expect(changed.incidents).toBe(opened.incidents)
    const closed = applyLiveEvent(changed, resolveEvent('b')).state
    expect(closed.incidents).toEqual({ active: [], resolved: [resolution('b')] })
    expect(closed.services).toBe(changed.services)
  })

  it('can establish an unseen resolution without a retained opening', () => {
    const result = applyLiveEvent(baseline(), resolveEvent())
    expect(result.successful).toBe(true)
    expect(result.state.incidents).toEqual({ active: [], resolved: [resolution()] })
  })

  it('makes duplicate openings, resolutions and stale reopenings idempotent', () => {
    const opened = applyLiveEvent(baseline(), openEvent()).state
    expect(applyLiveEvent(opened, openEvent()).state).toBe(opened)
    const closed = applyLiveEvent(opened, resolveEvent()).state
    expect(applyLiveEvent(closed, resolveEvent()).state).toBe(closed)
    expect(applyLiveEvent(closed, openEvent()).state).toBe(closed)
    expect(applyLiveEvent(closed, openEvent('a', t0)).state).toBe(closed)
  })

  it('preserves raw keys and never closes an active incident using a normalized identity', () => {
    const previous = baseline({ active: [opening()], resolved: [] })
    const alternateStart = '2026-11-11T08:00:01.123456000+08:00'
    const result = applyLiveEvent(previous, resolveEvent('a', alternateStart))
    expect(result.successful).toBe(false)
    expect(result.state).toBe(previous)
    expect(result.issues[0]).toMatchObject({ kind: 'incident-conflict', incidentKey: `a|${alternateStart}` })
    const closed = applyLiveEvent(previous, resolveEvent()).state
    expect(closed.incidents?.resolved[0].startedAt).toBe(t1)
  })

  it('retains accepted resolution strings for equivalent instants from SSE and REST', () => {
    const accepted = resolution()
    const previous = baseline({ active: [], resolved: [accepted] })
    const alternate = { ...accepted, resolvedAt: '2026-11-11T08:00:02.123456000+08:00' }
    expect(applyLiveEvent(previous, { type: 'incident.resolved', payload: alternate }).state).toBe(previous)
    const snapshot = reconcileSnapshots(previous, { incidents: { active: [], resolved: [alternate] } })
    expect(snapshot.successful).toBe(true)
    expect(snapshot.state).toBe(previous)
    expect(snapshot.state.incidents?.resolved[0]).toBe(accepted)
  })

  it.each(['event', 'snapshot'] as const)('rejects a changed accepted resolution from %s', source => {
    const previous = baseline({ active: [], resolved: [resolution()] })
    const incoming = resolution('a', t1, '2026-11-11T00:00:02.123456001Z')
    const result = source === 'event'
      ? applyLiveEvent(previous, { type: 'incident.resolved', payload: incoming })
      : reconcileSnapshots(previous, { incidents: { active: [], resolved: [incoming] } })
    expect(result.successful).toBe(false)
    expect(result.state).toBe(previous)
    expect(result.issues[0]).toMatchObject({ kind: 'resolution-conflict', incidentKey: `a|${t1}` })
  })

  it('rejects a REST lifecycle regression without rejecting an independent valid status slice', () => {
    const previous = baseline({ active: [], resolved: [resolution()] })
    const result = reconcileSnapshots(previous, {
      status: { services: [makeService('a', 'down', t3)] }, incidents: { active: [opening()], resolved: [] },
    })
    expect(result.successful).toBe(false)
    expect(result.state.incidents).toBe(previous.incidents)
    expect(result.state.lifecycle).toBe(previous.lifecycle)
    expect(result.state.services?.[0].status.state).toBe('down')
  })

  it('ignores older openings but diagnoses conflicting newer active incidents', () => {
    const previous = baseline({ active: [opening('a', t2)], resolved: [] })
    expect(applyLiveEvent(previous, openEvent('a', t1)).state).toBe(previous)
    const conflict = applyLiveEvent(previous, openEvent('a', t3))
    expect(conflict.successful).toBe(false)
    expect(conflict.state).toBe(previous)
    expect(conflict.issues[0].kind).toBe('incident-conflict')
  })

  it('allows an older nonoverlapping resolution alongside a newer active incident', () => {
    const previous = baseline({ active: [opening('a', t3)], resolved: [] })
    const result = applyLiveEvent(previous, resolveEvent('a', t1, t2))
    expect(result.successful).toBe(true)
    expect(result.state.incidents).toEqual({ active: [opening('a', t3)], resolved: [resolution()] })
    const overlap = applyLiveEvent(previous, resolveEvent('a', t1, t4))
    expect(overlap.successful).toBe(false)
    expect(overlap.state).toBe(previous)
  })

  it('rejects a resolution for a different newer identity while an older incident is active', () => {
    const previous = baseline({ active: [opening()], resolved: [] })
    const result = applyLiveEvent(previous, resolveEvent('a', t2, t3))
    expect(result.successful).toBe(false)
    expect(result.state).toBe(previous)
  })

  it('rejects a new opening that overlaps a known resolution', () => {
    const previous = baseline({ active: [], resolved: [resolution('a', t0, t3)] })
    const conflict = applyLiveEvent(previous, openEvent('a', t2))
    expect(conflict.successful).toBe(false)
    expect(conflict.state).toBe(previous)
    expect(applyLiveEvent(previous, openEvent('a', t4)).successful).toBe(true)
  })

  it('requires an incident baseline and refuses unknown speculative incident records', () => {
    const initial = createInitialReconciliationState()
    for (const event of [openEvent(), resolveEvent()]) {
      expect(applyLiveEvent(initial, event).issues[0].kind).toBe('missing-baseline')
      const previous = baseline()
      const unknown = { ...event, payload: { ...event.payload, serviceId: 'missing' } } as LiveEvent
      const result = applyLiveEvent(previous, unknown)
      expect(result.state).toBe(previous)
      expect(result.issues[0].kind).toBe('unknown-service')
    }
  })

  it('accepts REST legacy incidents and resolves known incidents without catalogue membership', () => {
    const previous = reconcileSnapshots(createInitialReconciliationState(), {
      incidents: { active: [opening('legacy')], resolved: [] },
    }).state
    expect(previous.services).toBeNull()
    const result = applyLiveEvent(previous, resolveEvent('legacy'))
    expect(result.successful).toBe(true)
    expect(result.state.incidents?.resolved).toEqual([resolution('legacy')])
    expect(applyLiveEvent(result.state, resolveEvent('legacy')).state).toBe(result.state)
  })

  it.each([
    { active: [opening(), opening()], resolved: [] },
    { active: [opening()], resolved: [resolution()] },
    { active: [opening(), opening('a', t2)], resolved: [] },
    { active: [], resolved: [resolution(), resolution()] },
  ])('rejects logically inconsistent incident snapshots atomically: %j', incidents => {
    const previous = baseline()
    const result = reconcileSnapshots(previous, { incidents })
    expect(result.successful).toBe(false)
    expect(result.state).toBe(previous)
    expect(result.issues.length).toBeGreaterThan(0)
  })
})

describe('snapshot absence and ordered replay', () => {
  it('retires only baseline active incidents, without fabricating resolved timestamps', () => {
    const previous = baseline({ active: [opening()], resolved: [] })
    const result = reconcileSnapshots(previous, { incidents: { active: [], resolved: [] } })
    expect(result.successful).toBe(true)
    expect(result.state.incidents).toEqual({ active: [], resolved: [] })
    expect(result.state.lifecycle.get('a')).toEqual({ closedThrough: t1, latestResolution: null })
    expect(applyLiveEvent(result.state, openEvent()).state).toBe(result.state)
    expect(applyLiveEvent(result.state, openEvent('a', t0)).state).toBe(result.state)
  })

  it('does not treat an omitted incident snapshot as absence evidence', () => {
    const previous = baseline({ active: [opening()], resolved: [] })
    const result = reconcileSnapshots(previous, { status: { services: [] } })
    expect(result.state.incidents).toBe(previous.incidents)
    expect(result.state.lifecycle.size).toBe(0)
  })

  it('keeps a new opening received during an empty initial incident snapshot', () => {
    const previous = reconcileSnapshots(createInitialReconciliationState(), { status: { services: [makeService('a', 'up')] } }).state
    const result = reconcileSnapshots(previous, { incidents: { active: [], resolved: [] } }, [openEvent()])
    expect(result.successful).toBe(true)
    expect(result.state.incidents?.active).toEqual([opening()])
    expect(result.state.lifecycle.size).toBe(0)
  })

  it('retires an absent baseline opening but accepts a legitimately newer buffered opening', () => {
    const previous = baseline({ active: [opening()], resolved: [] })
    const result = reconcileSnapshots(previous, { incidents: { active: [], resolved: [] } }, [openEvent(), openEvent('a', t3)])
    expect(result.successful).toBe(true)
    expect(result.state.incidents).toEqual({ active: [opening('a', t3)], resolved: [] })
    expect(result.state.lifecycle.get('a')?.closedThrough).toBe(t1)
  })

  it('accepts the actual resolution of a retired baseline identity', () => {
    const previous = baseline({ active: [opening()], resolved: [] })
    const result = reconcileSnapshots(previous, { incidents: { active: [], resolved: [] } }, [resolveEvent(), openEvent('a', t3)])
    expect(result.successful).toBe(true)
    expect(result.state.incidents).toEqual({ active: [opening('a', t3)], resolved: [resolution()] })
  })

  it('replays a full incident cycle absent from a non-atomic snapshot', () => {
    const previous = baseline()
    const result = reconcileSnapshots(previous, {
      status: { services: [makeService('a', 'down', t3), makeService('b', 'unknown')] },
      incidents: { active: [], resolved: [] },
    }, [statusEvent('a', 'up', t2), openEvent(), resolveEvent()])
    expect(result.successful).toBe(true)
    expect(result.state.services?.[0].status).toEqual({ state: 'down', lastCheckedAt: t3 })
    expect(result.state.incidents).toEqual({ active: [], resolved: [resolution()] })
  })

  it('handles an opening already resolved by REST before its buffered events replay', () => {
    const result = reconcileSnapshots(baseline(), { incidents: { active: [], resolved: [resolution()] } }, [openEvent(), resolveEvent()])
    expect(result.successful).toBe(true)
    expect(result.state.incidents).toEqual({ active: [], resolved: [resolution()] })
  })

  it('handles a newer REST active incident plus buffered older resolution and duplicate opening', () => {
    const previous = baseline({ active: [opening()], resolved: [] })
    const result = reconcileSnapshots(previous, { incidents: { active: [opening('a', t3)], resolved: [resolution()] } }, [resolveEvent(), openEvent('a', t3)])
    expect(result.successful).toBe(true)
    expect(result.state.incidents).toEqual({ active: [opening('a', t3)], resolved: [resolution()] })
  })

  it('preserves all diagnostics while processing later valid events and independent resources', () => {
    const previous = baseline({ active: [], resolved: [resolution()] })
    const result = reconcileSnapshots(previous, {
      incidents: { active: [], resolved: [resolution('a', t1, t3)] },
    }, [statusEvent('a', 'down', t1), statusEvent('b', 'up', t3)])
    expect(result.successful).toBe(false)
    expect(result.issues.map(issue => issue.kind)).toEqual(['resolution-conflict', 'status-conflict'])
    expect(result.state.incidents).toBe(previous.incidents)
    expect(result.state.services?.[1].status.state).toBe('up')
  })
})

describe('incident feed ordering, bounds and lifecycle evidence', () => {
  function history(count: number): IncidentResolvedEventPayload[] {
    return Array.from({ length: count }, (_, index) => resolution(
      `history-${index}`, `2026-10-${String(index + 1).padStart(2, '0')}T00:00:00.123456Z`,
      `2026-10-${String(index + 1).padStart(2, '0')}T00:01:00.123456Z`,
    ))
  }

  it('sorts active by start and resolved by resolution using precise instants', () => {
    const earliest = opening('old', '2026-11-11T08:00:00.1+08:00')
    const middle = opening('middle', '2026-11-11T00:00:00.100001Z')
    const newest = opening('new', '2026-11-10T19:00:00.100002-05:00')
    const first = resolution('r1', t0, '2026-11-11T08:00:02.123456+08:00')
    const second = resolution('r2', t0, '2026-11-11T00:00:02.123456001Z')
    const result = reconcileSnapshots(baseline(), { incidents: { active: [earliest, newest, middle], resolved: [first, second] } })
    expect(result.successful).toBe(true)
    expect(result.state.incidents?.active).toEqual([newest, middle, earliest])
    expect(result.state.incidents?.resolved).toEqual([second, first])
  })

  it('preserves REST tie order and inserts equal-time events after existing records', () => {
    const first = resolution('b')
    const second = resolution('a')
    const previous = reconcileSnapshots(baseline(), { incidents: { active: [], resolved: [first, second] } }).state
    expect(previous.incidents?.resolved).toEqual([first, second])
    const withCatalogue = reconcileSnapshots(previous, { status: { services: ['a', 'b', 'c', 'd'].map(id => makeService(id, 'up', t1)) } }).state
    const result = reconcileSnapshots(withCatalogue, {}, [resolveEvent('d'), resolveEvent('c')])
    expect(result.successful).toBe(true)
    expect(result.state.incidents?.resolved.map(incident => incident.serviceId)).toEqual(['b', 'a', 'd', 'c'])
    const actives = reconcileSnapshots(withCatalogue, { incidents: { active: [opening('b')], resolved: [] } }, [openEvent('a'), openEvent('d')])
    // The previous resolutions prohibit reopening those same identities.
    expect(actives.successful).toBe(false)
    const fresh = reconcileSnapshots(baseline(), { incidents: { active: [opening('b')], resolved: [] } }, [openEvent('a')])
    expect(fresh.state.incidents?.active.map(incident => incident.serviceId)).toEqual(['b', 'a'])
  })

  it.each([0, 1, 19, 20, 25])('retains every active incident and only remaining history capacity with %i active', count => {
    const active = Array.from({ length: count }, (_, index) => opening(`active-${index}`))
    const result = reconcileSnapshots(baseline(), { incidents: { active, resolved: history(25) } })
    expect(result.successful).toBe(true)
    expect(result.state.incidents?.active).toHaveLength(count)
    expect(result.state.incidents?.resolved).toHaveLength(Math.max(0, 20 - count))
    expect(result.state.incidents?.resolved[0]?.serviceId).toBe(count < 20 ? 'history-24' : undefined)
  })

  it('trims on live openings/resolutions and never manufactures history to fill capacity', () => {
    const previous = baseline({ active: [], resolved: history(20) })
    const opened = applyLiveEvent(previous, openEvent()).state
    expect(opened.incidents?.resolved).toHaveLength(19)
    const closed = applyLiveEvent(opened, resolveEvent()).state
    expect(closed.incidents?.active).toEqual([])
    expect(closed.incidents?.resolved).toHaveLength(20)
    expect(closed.incidents?.resolved[0]).toEqual(resolution())
    const replaced = reconcileSnapshots(closed, { incidents: { active: [], resolved: [] } }).state
    expect(replaced.incidents?.resolved).toEqual([])
  })

  it('keeps closure and immutable-resolution evidence after visible history is trimmed', () => {
    const active = Array.from({ length: 20 }, (_, index) => opening(`active-${index}`))
    const previous = baseline({ active, resolved: [] })
    const closed = applyLiveEvent(previous, resolveEvent()).state
    expect(closed.incidents).toBe(previous.incidents)
    expect(closed.lifecycle.get('a')?.latestResolution).toEqual(resolution())
    expect(applyLiveEvent(closed, openEvent()).state).toBe(closed)
    expect(applyLiveEvent(closed, resolveEvent()).state).toBe(closed)
    const conflict = applyLiveEvent(closed, resolveEvent('a', t1, t3))
    expect(conflict.successful).toBe(false)
    expect(conflict.state).toBe(closed)
    const restConflict = reconcileSnapshots(closed, { incidents: { active, resolved: [resolution('a', t1, t3)] } })
    expect(restConflict.successful).toBe(false)
    expect(restConflict.state).toBe(closed)
  })

  it('retains the latest actual resolution when a newer baseline incident is retired by absence', () => {
    const previous = baseline({ active: [opening('a', t3)], resolved: [resolution()] })
    const retired = reconcileSnapshots(previous, { incidents: { active: [], resolved: [] } }).state
    expect(retired.lifecycle.get('a')).toEqual({ closedThrough: t3, latestResolution: resolution() })
    expect(applyLiveEvent(retired, resolveEvent('a', t1, t4)).successful).toBe(false)
    expect(applyLiveEvent(retired, openEvent('a', t3)).state).toBe(retired)
    const actualResolution = applyLiveEvent(retired, resolveEvent('a', t3, t4))
    expect(actualResolution.successful).toBe(true)
    expect(actualResolution.state.incidents?.resolved).toEqual([resolution('a', t3, t4)])
  })

  it('ignores unseen stale resolutions but lets authoritative REST restore older trimmed history', () => {
    const previous = baseline({ active: [], resolved: [resolution('a', t3, t4)] })
    expect(applyLiveEvent(previous, resolveEvent()).state).toBe(previous)
    const result = reconcileSnapshots(previous, { incidents: { active: [], resolved: [resolution('a', t3, t4), resolution()] } })
    expect(result.successful).toBe(true)
    expect(result.state.incidents?.resolved).toHaveLength(2)
    expect(result.state.lifecycle.get('a')?.closedThrough).toBe(t3)
  })

  it('prunes evidence only when neither the catalogue nor retained incidents reference the service', () => {
    const previous = baseline({ active: [], resolved: [resolution('a'), resolution('legacy')] })
    expect(previous.lifecycle.size).toBe(2)
    const noCatalogue = reconcileSnapshots(previous, { status: { services: [] } }).state
    expect(noCatalogue.lifecycle.size).toBe(2)
    const noIncidents = reconcileSnapshots(noCatalogue, { incidents: { active: [], resolved: [] } }).state
    expect(noIncidents.lifecycle.size).toBe(0)
    const catalogueStillPresent = reconcileSnapshots(previous, { incidents: { active: [], resolved: [] } }).state
    expect([...catalogueStillPresent.lifecycle.keys()]).toEqual(['a'])
  })
})

describe('input immutability and reference preservation', () => {
  it('does not mutate frozen REST DTOs, arrays, events or accepted state', () => {
    const service = makeService('a', 'up', t1)
    Object.freeze(service.status)
    Object.freeze(service)
    const services = [service]
    Object.freeze(services)
    const incident = Object.freeze(opening())
    const active = [incident]
    const resolved: IncidentResolvedEventPayload[] = []
    Object.freeze(active)
    Object.freeze(resolved)
    const incidents = Object.freeze({ active, resolved })
    const state = Object.freeze(reconcileSnapshots(createInitialReconciliationState(), { status: { services }, incidents }).state)
    const beforeEvidence = [...state.lifecycle]
    const event = Object.freeze({ type: 'incident.resolved' as const, payload: Object.freeze(resolution()) })
    const result = applyLiveEvent(state, event)
    expect(result.successful).toBe(true)
    expect(state.incidents?.active).toEqual([opening()])
    expect([...state.lifecycle]).toEqual(beforeEvidence)
    expect(state.services?.[0]).toBe(service)
    expect(result.state.services).toBe(state.services)
    expect(result.state.incidents?.resolved[0]).toBe(event.payload)
  })

  it('reuses equivalent snapshot DTOs, slices and lifecycle maps', () => {
    const previous = baseline({ active: [opening('b')], resolved: [resolution()] })
    const result = reconcileSnapshots(previous, {
      status: { services: [makeService('a', 'up', t1), makeService('b', 'unknown')] },
      incidents: { active: [opening('b')], resolved: [resolution()] },
    })
    expect(result.successful).toBe(true)
    expect(result.state).toBe(previous)
  })

  it('preserves other incident references when one incident resolves', () => {
    const previous = baseline({ active: [opening('a'), opening('b')], resolved: [] })
    const result = applyLiveEvent(previous, resolveEvent())
    expect(result.state.incidents?.active[0]).toBe(previous.incidents?.active[1])
    expect(result.state.services).toBe(previous.services)
    expect(previous.lifecycle.size).toBe(0)
  })
})
