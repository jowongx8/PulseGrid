import { describe, expect, it } from 'vitest'
import { makeService } from '@/test/services'
import type { LiveEvent } from './events'
import { createInitialReconciliationResult, reconciliationReducer } from './reducer'

const t1 = '2026-11-11T00:00:01.123456Z'
const t2 = '2026-11-11T00:00:02.123456Z'
const event: LiveEvent = { type: 'status.changed', payload: { serviceId: 'a', state: 'down', lastCheckedAt: t2 } }

describe('reconciliation reducer', () => {
  it('creates independent empty states with no fabricated resources', () => {
    const first = createInitialReconciliationResult()
    const second = createInitialReconciliationResult()
    expect(first).toMatchObject({ successful: true, issues: [], state: { services: null, incidents: null } })
    expect(first.state.lifecycle).not.toBe(second.state.lifecycle)
  })

  it('accepts partial snapshots and ordered supplied events in one pure action', () => {
    const previous = createInitialReconciliationResult()
    const result = reconciliationReducer(previous, {
      type: 'snapshots', snapshots: { status: { services: [makeService('a', 'up', t1)] } }, events: [event],
    })
    expect(result.successful).toBe(true)
    expect(result.state.services?.[0].status).toEqual({ state: 'down', lastCheckedAt: t2 })
    expect(result.state.incidents).toBeNull()
    expect(previous.state.services).toBeNull()
  })

  it('applies live events without changing unrelated resources', () => {
    const previous = reconciliationReducer(createInitialReconciliationResult(), {
      type: 'snapshots', snapshots: { status: { services: [makeService('a', 'up', t1)] }, incidents: { active: [], resolved: [] } },
    })
    const result = reconciliationReducer(previous, { type: 'event', event })
    expect(result.state.incidents).toBe(previous.state.incidents)
    expect(result.state.services?.[0].status.state).toBe('down')
    expect(previous.state.services?.[0].status.state).toBe('up')
    expect(reconciliationReducer(result, { type: 'event', event }).state).toBe(result.state)
  })

  it('reports operation diagnostics without implementing connection health or retries', () => {
    const failed = reconciliationReducer(createInitialReconciliationResult(), { type: 'event', event })
    expect(failed.successful).toBe(false)
    expect(failed.issues[0].kind).toBe('missing-baseline')
    const recovered = reconciliationReducer(failed, { type: 'snapshots', snapshots: { status: { services: [makeService('a', 'up', t1)] } } })
    expect(recovered.successful).toBe(true)
    expect(recovered.issues).toEqual([])
    expect(failed.successful).toBe(false)
    expect(recovered).not.toHaveProperty('connection')
  })

  it('retains conflicts from a snapshot-and-events action even after successful later events', () => {
    const result = reconciliationReducer(createInitialReconciliationResult(), {
      type: 'snapshots', snapshots: { status: { services: [makeService('a', 'up', t1)] } },
      events: [{ ...event, payload: { ...event.payload, lastCheckedAt: t1 } }, event],
    })
    expect(result.successful).toBe(false)
    expect(result.issues).toHaveLength(1)
    expect(result.issues[0].kind).toBe('status-conflict')
    expect(result.state.services?.[0].status.state).toBe('down')
  })
})
