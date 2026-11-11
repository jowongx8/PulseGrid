import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IncidentsResponse, ServiceState, StatusResponse } from '../contracts'
import { makeService } from '@/test/services'
import { fetchIncidentsSnapshot, fetchStatusSnapshot } from './api'
import { createSSEClient, type LiveEvent, type SSEClientOptions } from './events'
import { createLiveStatusCoordinator } from './coordinator'
import * as reducerModule from './reducer'

vi.mock('./api', () => ({ fetchStatusSnapshot: vi.fn(), fetchIncidentsSnapshot: vi.fn() }))
vi.mock('./events', () => ({ createSSEClient: vi.fn() }))

const t1 = '2026-11-11T00:00:01.123456Z'
const t2 = '2026-11-11T00:00:02.123456Z'
const t3 = '2026-11-11T00:00:03.123456Z'
const emptyIncidents: IncidentsResponse = { active: [], resolved: [] }
const status: StatusResponse = { services: [makeService('a', 'up', t1), makeService('b', 'unknown')] }
const malformed = { eventType: 'status.changed' as const, kind: 'invalid-json' as const, cause: new SyntaxError('Invalid JSON') }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

function request<T>(signal?: AbortSignal) {
  return { ...deferred<T>(), signal }
}

let statusRequests: ReturnType<typeof request<StatusResponse>>[]
let incidentRequests: ReturnType<typeof request<IncidentsResponse>>[]
let sources: { options: SSEClientOptions; close: ReturnType<typeof vi.fn>; closed: boolean }[]
let stops: (() => void)[]

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-11T01:00:00Z'))
  vi.spyOn(Math, 'random').mockReturnValue(0.5)
  statusRequests = []
  incidentRequests = []
  sources = []
  stops = []
  vi.mocked(fetchStatusSnapshot).mockReset().mockImplementation(signal => {
    const pending = request<StatusResponse>(signal)
    statusRequests.push(pending)
    return pending.promise
  })
  vi.mocked(fetchIncidentsSnapshot).mockReset().mockImplementation(signal => {
    const pending = request<IncidentsResponse>(signal)
    incidentRequests.push(pending)
    return pending.promise
  })
  vi.mocked(createSSEClient).mockReset().mockImplementation(options => {
    expect(sources.filter(source => !source.closed)).toHaveLength(0)
    const source = { options, closed: false, close: vi.fn() }
    source.close.mockImplementation(() => {
      source.closed = true
      options.onConnection({ type: 'closed' })
    })
    sources.push(source)
    return { close: source.close }
  })
})

afterEach(() => {
  stops.forEach(stop => stop())
  vi.clearAllTimers()
  vi.useRealTimers()
})

function start() {
  const owner = createLiveStatusCoordinator()
  stops.push(owner.start())
  return owner
}

function open(index = sources.length - 1, reconnected = false) {
  sources[index].options.onConnection({ type: 'open', reconnected })
}

function interrupt(readyState = 0, index = sources.length - 1) {
  sources[index].options.onConnection({ type: 'transport-error', readyState })
}

function event(state: ServiceState = 'down', at = t2, serviceId = 'a'): LiveEvent {
  return { type: 'status.changed', payload: { serviceId, state, lastCheckedAt: at } }
}

function send(value: LiveEvent, index = sources.length - 1) {
  sources[index].options.onEvent(value)
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
}

async function settle(index = statusRequests.length - 1, services = status, incidents = emptyIncidents) {
  statusRequests[index].resolve(services)
  incidentRequests[index].resolve(incidents)
  await flush()
}

async function fail(index = statusRequests.length - 1) {
  statusRequests[index].reject(new Error('Status unavailable'))
  incidentRequests[index].reject(new Error('Incidents unavailable'))
  await flush()
}

async function live() {
  const owner = start()
  open()
  await settle()
  expect(owner.getSnapshot().synchronization).toBe('synchronized')
  return owner
}

describe('startup and synchronization barriers', () => {
  it('constructs a dormant owner and caches immutable snapshots', () => {
    const owner = createLiveStatusCoordinator()
    expect(owner.getSnapshot()).toBe(owner.getSnapshot())
    expect(owner.getSnapshot()).toMatchObject({ services: null, incidents: null, synchronization: 'waiting' })
    expect(createSSEClient).not.toHaveBeenCalled()
    expect(fetchStatusSnapshot).not.toHaveBeenCalled()
  })

  it('opens SSE first, fetches concurrently and publishes only one complete batch', async () => {
    const owner = start()
    expect(sources).toHaveLength(1)
    expect(statusRequests).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(1_999)
    expect(statusRequests).toHaveLength(0)
    open()
    expect(statusRequests).toHaveLength(1)
    expect(incidentRequests).toHaveLength(1)
    expect(statusRequests[0].signal).toBe(incidentRequests[0].signal)
    statusRequests[0].resolve(status)
    await flush()
    expect(owner.getSnapshot().services).toBeNull()
    expect(owner.getSnapshot().synchronization).toBe('synchronizing')
    incidentRequests[0].resolve(emptyIncidents)
    await flush()
    expect(owner.getSnapshot()).toMatchObject({ services: status.services, incidents: emptyIncidents, transport: 'open', synchronization: 'synchronized' })
    expect(vi.getTimerCount()).toBe(0)
  })

  it('installs the barrier before invoking REST and captures events between responses', async () => {
    const owner = start()
    const original = vi.mocked(fetchStatusSnapshot).getMockImplementation()!
    vi.mocked(fetchStatusSnapshot).mockImplementationOnce(signal => {
      send(event('down', t2))
      return original(signal)
    })
    open()
    statusRequests[0].resolve(status)
    await flush()
    send(event('up', t3))
    expect(owner.getSnapshot().services).toBeNull()
    incidentRequests[0].resolve(emptyIncidents)
    await flush()
    expect(owner.getSnapshot().services?.[0].status).toEqual({ state: 'up', lastCheckedAt: t3 })
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('routes events delivered by a commit subscriber directly after the drained batch', async () => {
    const owner = start()
    const reducer = vi.spyOn(reducerModule, 'reconciliationReducer')
    let injected = false
    owner.subscribe(() => {
      if (!injected && owner.getSnapshot().synchronization === 'synchronized') {
        injected = true
        send(event('up', t3))
      }
    })
    open()
    send(event())
    await settle()
    const batches = reducer.mock.calls.filter(([, action]) => action.type === 'snapshots')
    expect(batches).toHaveLength(1)
    expect(batches[0][1]).toMatchObject({ events: [event()] })
    expect(reducer.mock.calls.filter(([, action]) => action.type === 'event')).toHaveLength(1)
    expect(owner.getSnapshot().services?.[0].status.lastCheckedAt).toBe(t3)
  })

  it('keeps a new post-barrier incident absent from the returned snapshot', async () => {
    const owner = start()
    open()
    const opening: LiveEvent = { type: 'incident.opened', payload: { serviceId: 'a', startedAt: t2, resolvedAt: null } }
    send(opening)
    await settle()
    expect(owner.getSnapshot().incidents?.active).toEqual([opening.payload])
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('treats a first open preceded by errors as requiring fresh synchronization', async () => {
    const owner = start()
    interrupt()
    expect(owner.getSnapshot().transport).toBe('reconnecting')
    open(0, false)
    expect(statusRequests).toHaveLength(1)
    expect(owner.getSnapshot().synchronization).toBe('synchronizing')
    await settle()
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('coalesces repeated opens while the same connection is already open', async () => {
    const owner = start()
    open()
    open()
    expect(statusRequests).toHaveLength(1)
    await settle()
    open()
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
    expect(statusRequests).toHaveLength(1)
  })
})

describe('partial outcomes and deadlines', () => {
  it.each(['status', 'incidents'] as const)('preserves successful data when %s fails', async failed => {
    const owner = start()
    open()
    const error = new Error('Unavailable')
    if (failed === 'status') {
      statusRequests[0].reject(error)
      incidentRequests[0].resolve(emptyIncidents)
    } else {
      statusRequests[0].resolve(status)
      incidentRequests[0].reject(error)
    }
    await flush()
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    expect(owner.getSnapshot().errors[failed]).toBe(error)
    expect(owner.getSnapshot().services).toEqual(failed === 'status' ? null : status.services)
    expect(owner.getSnapshot().incidents).toEqual(failed === 'incidents' ? null : emptyIncidents)
    await vi.advanceTimersByTimeAsync(1_000)
    await settle()
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
    expect(owner.getSnapshot().errors[failed]).toBeNull()
  })

  it('retains last-known data when both endpoints fail', async () => {
    const owner = await live()
    const before = owner.getSnapshot()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    await fail()
    expect(owner.getSnapshot().services).toBe(before.services)
    expect(owner.getSnapshot().incidents).toBe(before.incidents)
    expect(owner.getSnapshot().errors.status).toBeInstanceOf(Error)
    expect(owner.getSnapshot().errors.incidents).toBeInstanceOf(Error)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
  })

  it.each(['status', 'incidents', 'both'] as const)('enforces the deadline with %s pending and ignores cancellation-resistant results', async pending => {
    const owner = start()
    open()
    if (pending !== 'status' && pending !== 'both') statusRequests[0].resolve(status)
    if (pending !== 'incidents' && pending !== 'both') incidentRequests[0].resolve(emptyIncidents)
    send(event())
    await flush()
    await vi.advanceTimersByTimeAsync(9_999)
    expect(owner.getSnapshot().services).toBeNull()
    await vi.advanceTimersByTimeAsync(1)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    expect(statusRequests[0].signal?.aborted).toBe(true)
    if (pending !== 'incidents') expect(owner.getSnapshot().errors.status?.name).toBe('TimeoutError')
    if (pending !== 'status') expect(owner.getSnapshot().errors.incidents?.name).toBe('TimeoutError')
    if (pending === 'incidents') expect(owner.getSnapshot().services?.[0].status.state).toBe('down')
    const afterDeadline = owner.getSnapshot()
    await settle(0)
    expect(owner.getSnapshot()).toBe(afterDeadline)
    await vi.advanceTimersByTimeAsync(1_000)
    await settle(1)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('accepts independent incident data when the status snapshot conflicts', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    const incident = { serviceId: 'b', startedAt: t2, resolvedAt: null }
    await settle(1, { services: [makeService('a', 'down', t1)] }, { active: [incident], resolved: [] })
    expect(owner.getSnapshot().services).toEqual(status.services)
    expect(owner.getSnapshot().incidents?.active).toEqual([incident])
    expect(owner.getSnapshot().errors.reconciliation[0].kind).toBe('status-conflict')
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
  })
})

describe('REST-only startup', () => {
  it('loads after grace, remains degraded, and never polls after a complete fallback', async () => {
    const owner = start()
    await vi.advanceTimersByTimeAsync(2_000)
    expect(statusRequests).toHaveLength(1)
    await settle()
    expect(owner.getSnapshot()).toMatchObject({ services: status.services, incidents: emptyIncidents, transport: 'connecting', synchronization: 'unsynchronized' })
    await vi.advanceTimersByTimeAsync(300_000)
    expect(statusRequests).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
    open()
    expect(statusRequests).toHaveLength(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronizing')
    await settle(1)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('retries partial fallback failure and stops once a complete fallback succeeds', async () => {
    const owner = start()
    await vi.advanceTimersByTimeAsync(2_000)
    statusRequests[0].resolve(status)
    incidentRequests[0].reject(new Error('Unavailable'))
    await flush()
    expect(owner.getSnapshot().services).toEqual(status.services)
    await vi.advanceTimersByTimeAsync(1_000)
    await settle(1)
    await vi.advanceTimersByTimeAsync(120_000)
    expect(statusRequests).toHaveLength(2)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
  })

  it('supersedes an in-flight fallback on open and ignores its late success', async () => {
    const owner = start()
    await vi.advanceTimersByTimeAsync(2_000)
    statusRequests[0].resolve(status)
    await flush()
    open()
    expect(statusRequests[0].signal?.aborted).toBe(true)
    expect(statusRequests).toHaveLength(2)
    send(event())
    await settle(1)
    const accepted = owner.getSnapshot()
    incidentRequests[0].resolve({ active: [{ serviceId: 'legacy', startedAt: t1, resolvedAt: null }], resolved: [] })
    await flush()
    expect(owner.getSnapshot()).toBe(accepted)
    expect(accepted.synchronization).toBe('synchronized')
  })

  it('does not let repeated native startup errors starve a REST-only request', async () => {
    const owner = start()
    await vi.advanceTimersByTimeAsync(2_000)
    interrupt()
    interrupt()
    expect(statusRequests[0].signal?.aborted).toBe(false)
    await settle()
    expect(owner.getSnapshot().services).toEqual(status.services)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    await vi.advanceTimersByTimeAsync(60_000)
    expect(statusRequests).toHaveLength(1)
  })

  it('loads fallback after grace even when every EventSource construction fails', async () => {
    vi.mocked(createSSEClient).mockImplementation(() => { throw new Error('Unsupported EventSource') })
    const owner = start()
    await vi.advanceTimersByTimeAsync(2_000)
    expect(statusRequests).toHaveLength(1)
    await settle()
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    expect(owner.getSnapshot().services).toEqual(status.services)
    await vi.advanceTimersByTimeAsync(120_000)
    expect(vi.mocked(createSSEClient).mock.calls.length).toBeGreaterThan(2)
    expect(statusRequests).toHaveLength(1)
  })

  it('retries failed fallback without starvation by repeatedly failed terminal replacements', async () => {
    vi.mocked(createSSEClient).mockImplementation(() => { throw new Error('Unsupported EventSource') })
    const owner = start()
    await vi.advanceTimersByTimeAsync(2_000)
    await fail()
    await vi.advanceTimersByTimeAsync(1_000)
    expect(statusRequests).toHaveLength(2)
    await settle(1)
    expect(owner.getSnapshot().services).toEqual(status.services)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
  })

  it('still falls back if an early opening disconnects before startup completes', async () => {
    const owner = start()
    open()
    await vi.advanceTimersByTimeAsync(500)
    interrupt()
    expect(statusRequests[0].signal?.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(1_500)
    expect(statusRequests).toHaveLength(2)
    await settle(1)
    expect(owner.getSnapshot().services).toEqual(status.services)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
  })
})

describe('direct processing and sticky health', () => {
  it('applies direct updates, incident cycles and normal duplicates without recovery', async () => {
    const owner = await live()
    const metadata = owner.getSnapshot().services?.[0]
    send(event())
    send({ type: 'incident.opened', payload: { serviceId: 'a', startedAt: t2, resolvedAt: null } })
    send({ type: 'incident.resolved', payload: { serviceId: 'a', startedAt: t2, resolvedAt: t3 } })
    send(event())
    send(event('up', t1))
    expect(owner.getSnapshot().services?.[0]).toMatchObject({ name: metadata?.name, weight: metadata?.weight, status: { state: 'down', lastCheckedAt: t2 } })
    expect(owner.getSnapshot().incidents?.resolved).toEqual([{ serviceId: 'a', startedAt: t2, resolvedAt: t3 }])
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
    expect(vi.getTimerCount()).toBe(0)
    expect(statusRequests).toHaveLength(1)
  })

  it('does not let a later valid event clear a conflict or establish Live', async () => {
    const owner = await live()
    send(event('down', t1))
    const issues = owner.getSnapshot().errors.reconciliation
    send(event('down', t2))
    expect(owner.getSnapshot().errors.reconciliation).toBe(issues)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    await vi.advanceTimersByTimeAsync(1_000)
    await settle(1)
    expect(owner.getSnapshot().services?.[0].status.state).toBe('down')
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
    expect(owner.getSnapshot().errors.reconciliation).toEqual([])
  })

  it('keeps complete REST attempts unhealthy when their buffered batch contains a conflict', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    send(event('down', t1))
    send(event('down', t2))
    await settle(1)
    expect(owner.getSnapshot().services?.[0].status.state).toBe('down')
    expect(owner.getSnapshot().errors.reconciliation[0].kind).toBe('status-conflict')
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    await vi.advanceTimersByTimeAsync(2_000)
    await settle(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('preserves accepted incident resolutions and recovers only after fresh corroborating snapshots', async () => {
    const owner = await live()
    const accepted = { serviceId: 'a', startedAt: t1, resolvedAt: t2 }
    send({ type: 'incident.resolved', payload: accepted })
    send({ type: 'incident.resolved', payload: { ...accepted, resolvedAt: t3 } })
    expect(owner.getSnapshot().incidents?.resolved[0]).toBe(accepted)
    expect(owner.getSnapshot().errors.reconciliation[0].kind).toBe('resolution-conflict')
    send(event())
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    await vi.advanceTimersByTimeAsync(1_000)
    await settle(1, status, { active: [], resolved: [{ ...accepted }] })
    expect(owner.getSnapshot().incidents?.resolved[0]).toBe(accepted)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('applies safe events without a queue between failed attempts and uses a new baseline on retry', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    await fail(1)
    send(event('down', t2))
    send({ type: 'incident.opened', payload: { serviceId: 'a', startedAt: t2, resolvedAt: null } })
    expect(owner.getSnapshot().incidents?.active).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(2_000)
    send({ type: 'incident.opened', payload: { serviceId: 'a', startedAt: t3, resolvedAt: null } })
    await settle(2)
    expect(owner.getSnapshot().incidents?.active).toEqual([{ serviceId: 'a', startedAt: t3, resolvedAt: null }])
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('reports missing baselines between failures without retaining an event queue', async () => {
    const owner = start()
    open()
    await fail()
    for (let index = 0; index < 300; index++) send(event())
    expect(owner.getSnapshot().errors.reconciliation[0].kind).toBe('missing-baseline')
    expect(owner.getSnapshot().errors.recovery).toBeNull()
    expect(statusRequests).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1_000)
    await settle(1)
    expect(owner.getSnapshot().services?.[0].status.state).toBe('up')
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('coalesces repeated incompatible payloads and retains the open native client', async () => {
    const owner = await live()
    for (let index = 0; index < 50; index++) sources[0].options.onPayloadError(malformed)
    expect(vi.getTimerCount()).toBe(1)
    expect(sources[0].close).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1_000)
    expect(statusRequests).toHaveLength(2)
    for (let index = 0; index < 50; index++) sources[0].options.onPayloadError(malformed)
    expect(statusRequests[1].signal?.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(1_999)
    expect(statusRequests).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(statusRequests).toHaveLength(3)
    await settle(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
    expect(owner.getSnapshot().errors.stream).toBeNull()
    expect(sources).toHaveLength(1)
  })
})

describe('connection recovery and backoff', () => {
  it('lets native reconnection run and requires new snapshots after reopening', async () => {
    const owner = await live()
    const before = owner.getSnapshot().services
    interrupt()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sources).toHaveLength(1)
    expect(statusRequests).toHaveLength(1)
    expect(owner.getSnapshot().services).toBe(before)
    open(0, true)
    expect(statusRequests).toHaveLength(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronizing')
    await settle(1)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('honors a pending recovery delay on reopening without overlapping attempts', async () => {
    const owner = await live()
    interrupt()
    open(0, true)
    open(0, true)
    expect(statusRequests).toHaveLength(1)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    await vi.advanceTimersByTimeAsync(1_000)
    expect(statusRequests).toHaveLength(2)
    await settle(1)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('disposes terminal clients before replacement and ignores their callbacks', async () => {
    const owner = await live()
    interrupt(2)
    expect(sources[0].close).toHaveBeenCalledOnce()
    const before = owner.getSnapshot()
    open(0, true)
    send(event(), 0)
    sources[0].options.onPayloadError(malformed)
    expect(owner.getSnapshot()).toBe(before)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(sources).toHaveLength(2)
    open(1)
    await settle(1)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('recovers from synchronous EventSource construction failure while loading fallback data', async () => {
    vi.mocked(createSSEClient).mockImplementationOnce(() => { throw new Error('Not supported') })
    const owner = start()
    expect(owner.getSnapshot().errors.stream).toMatchObject({ kind: 'setup-error' })
    await vi.advanceTimersByTimeAsync(1_000)
    expect(createSSEClient).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1_000)
    await settle()
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    open()
    await settle(1)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('caps exponential retry progression and resets it only after complete live success', async () => {
    const owner = start()
    open()
    for (const [index, delay] of [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000].entries()) {
      await fail(index)
      await vi.advanceTimersByTimeAsync(delay - 1)
      expect(statusRequests).toHaveLength(index + 1)
      await vi.advanceTimersByTimeAsync(1)
      expect(statusRequests).toHaveLength(index + 2)
    }
    await settle()
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(999)
    expect(statusRequests).toHaveLength(8)
    await vi.advanceTimersByTimeAsync(1)
    expect(statusRequests).toHaveLength(9)
  })

  it.each([{ random: 0, delay: 800 }, { random: 1, delay: 1_200 }])('uses controlled jitter $random with delay $delay', async ({ random, delay }) => {
    vi.mocked(Math.random).mockReturnValue(random)
    await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(delay - 1)
    expect(statusRequests).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(statusRequests).toHaveLength(2)
  })

  it('never exceeds the hard jitter ceiling', async () => {
    vi.mocked(Math.random).mockReturnValue(1)
    start()
    open()
    for (const delay of [1_200, 2_400, 4_800, 9_600, 19_200, 30_000]) {
      await fail()
      const count = statusRequests.length
      await vi.advanceTimersByTimeAsync(delay)
      expect(statusRequests).toHaveLength(count + 1)
    }
  })
})

describe('invalidation, reentrancy and obsolete callbacks', () => {
  it('replays only once before abort callbacks deliver new events and obsolete request results', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    const reducer = vi.spyOn(reducerModule, 'reconciliationReducer')
    send(event())
    const pending = statusRequests[1]
    pending.signal?.addEventListener('abort', () => {
      expect(owner.getSnapshot().services?.[0].status.state).toBe('down')
      send(event('up', t3))
      pending.resolve({ services: [makeService('a', 'unknown', t3)] })
      incidentRequests[1].reject(new Error('Late failure'))
    })
    sources[0].options.onPayloadError(malformed)
    await flush()
    const actions = reducer.mock.calls.map(([, action]) => action)
    expect(actions.filter(action => action.type === 'snapshots')).toEqual([{ type: 'snapshots', snapshots: {}, events: [event()] }])
    expect(actions.filter(action => action.type === 'event')).toEqual([{ type: 'event', event: event('up', t3) }])
    expect(owner.getSnapshot().services?.[0].status).toEqual({ state: 'up', lastCheckedAt: t3 })
    expect(owner.getSnapshot().errors.status).toBeNull()
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    await vi.advanceTimersByTimeAsync(2_000)
    await settle(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('routes subscriber-delivered events after replay without adding them to the retired buffer', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    send(event())
    let delivered = false
    owner.subscribe(() => {
      if (!delivered && owner.getSnapshot().services?.[0].status.lastCheckedAt === t2) {
        delivered = true
        send(event('up', t3))
      }
    })
    sources[0].options.onPayloadError(malformed)
    expect(delivered).toBe(true)
    expect(owner.getSnapshot().services?.[0].status.lastCheckedAt).toBe(t3)
    await settle(1)
    expect(owner.getSnapshot().services?.[0].status.lastCheckedAt).toBe(t3)
  })

  it('finishes aborting a detached attempt when a replay subscriber synchronously stops the owner', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    send(event())
    owner.subscribe(() => {
      if (owner.getSnapshot().services?.[0].status.lastCheckedAt === t2) stops[0]()
    })
    statusRequests[1].signal?.addEventListener('abort', () => send(event('up', t3)))
    sources[0].options.onPayloadError(malformed)
    const stopped = owner.getSnapshot()
    expect(statusRequests[1].signal?.aborted).toBe(true)
    expect(sources[0].close).toHaveBeenCalledOnce()
    await settle(1)
    expect(owner.getSnapshot()).toBe(stopped)
    expect(stopped.services?.[0].status.state).toBe('down')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('retires a terminal client before abort and close can reenter its callbacks', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    send(event())
    statusRequests[1].signal?.addEventListener('abort', () => {
      open(0, true)
      send(event('up', t3), 0)
      interrupt(2, 0)
    })
    interrupt(2)
    expect(sources[0].close).toHaveBeenCalledOnce()
    expect(owner.getSnapshot().transport).toBe('closed')
    expect(owner.getSnapshot().services?.[0].status.state).toBe('down')
    expect(statusRequests).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(2_000)
    open(1)
    await settle(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('handles reopening from an abort listener without skipping the recovery delay', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    statusRequests[1].signal?.addEventListener('abort', () => open(0, true))
    interrupt()
    expect(owner.getSnapshot().transport).toBe('open')
    expect(statusRequests).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(2_000)
    expect(statusRequests).toHaveLength(3)
    await settle(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it.each([false, true])('bounds the buffer and drains once, with a completed response: %s', async partial => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    if (partial) {
      statusRequests[1].resolve({ services: [makeService('a', 'up', '2026-11-11T00:00:59Z')] })
      await flush()
    }
    const reducer = vi.spyOn(reducerModule, 'reconciliationReducer')
    for (let index = 0; index < 257; index++) {
      send(event(index % 2 === 0 ? 'down' : 'up', `2026-11-11T00:00:02.${String(index + 1).padStart(9, '0')}Z`))
    }
    const snapshots = reducer.mock.calls.filter(([, action]) => action.type === 'snapshots')
    expect(snapshots).toHaveLength(1)
    expect(snapshots[0][1].type === 'snapshots' && snapshots[0][1].events).toHaveLength(256)
    expect(reducer.mock.calls.filter(([, action]) => action.type === 'event')).toHaveLength(1)
    expect(statusRequests[1].signal?.aborted).toBe(true)
    expect(owner.getSnapshot().services?.[0].status.lastCheckedAt).toBe('2026-11-11T00:00:02.000000257Z')
    expect(owner.getSnapshot().errors.recovery).toBe('buffer-overflow')
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    await settle(1)
    expect(owner.getSnapshot().services?.[0].status.lastCheckedAt).toBe('2026-11-11T00:00:02.000000257Z')
    await vi.advanceTimersByTimeAsync(2_000)
    await settle(2)
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
  })

  it('ignores late successes and failures after a newer generation has synchronized', async () => {
    const owner = await live()
    sources[0].options.onPayloadError(malformed)
    await vi.advanceTimersByTimeAsync(1_000)
    interrupt()
    open(0, true)
    await vi.advanceTimersByTimeAsync(2_000)
    await settle(2)
    const before = owner.getSnapshot()
    statusRequests[1].resolve({ services: [] })
    incidentRequests[1].reject(new Error('Obsolete failure'))
    await flush()
    expect(owner.getSnapshot()).toBe(before)
  })

  it('does not launch the second endpoint after synchronous cleanup during the first request', () => {
    const owner = start()
    const original = vi.mocked(fetchStatusSnapshot).getMockImplementation()!
    vi.mocked(fetchStatusSnapshot).mockImplementationOnce(signal => {
      const promise = original(signal)
      stops[0]()
      return promise
    })
    open()
    expect(statusRequests).toHaveLength(1)
    expect(statusRequests[0].signal?.aborted).toBe(true)
    expect(incidentRequests).toHaveLength(0)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('visibility and cleanup', () => {
  function visible() {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
  }

  it('notifies retained subscribers only after resources have been cleaned up', () => {
    const owner = start()
    open()
    const listener = vi.fn(() => {
      expect(owner.getSnapshot()).toMatchObject({ transport: 'closed', synchronization: 'unsynchronized' })
      expect(statusRequests[0].signal?.aborted).toBe(true)
      expect(sources[0].close).toHaveBeenCalledOnce()
      expect(vi.getTimerCount()).toBe(0)
    })
    owner.subscribe(listener)
    stops[0]()
    expect(listener).toHaveBeenCalledOnce()
    const stopped = owner.getSnapshot()
    stops[0]()
    expect(owner.getSnapshot()).toBe(stopped)
    expect(listener).toHaveBeenCalledOnce()
  })

  it('notifies remaining subscribers when another subscriber stops during publication', async () => {
    const owner = await live()
    owner.subscribe(() => {
      if (owner.getSnapshot().transport === 'open') stops[0]()
    })
    const seen: string[] = []
    owner.subscribe(() => seen.push(owner.getSnapshot().transport))
    send(event())
    expect(seen).toEqual(['closed'])
  })

  it('allows stop subscribers to restart without stale notifications or cleanup affecting the new session', () => {
    const owner = start()
    let restarted = false
    const unsubscribeRestart = owner.subscribe(() => {
      if (!restarted && owner.getSnapshot().transport === 'closed') {
        restarted = true
        expect(sources[0].close).toHaveBeenCalledOnce()
        stops[0]()
        stops.push(owner.start())
      }
    })
    const seen: string[] = []
    const unsubscribe = owner.subscribe(() => seen.push(owner.getSnapshot().transport))
    stops[0]()
    expect(sources).toHaveLength(2)
    expect(seen).toEqual(['connecting'])
    expect(sources[1].closed).toBe(false)
    stops[0]()
    expect(sources[1].closed).toBe(false)
    unsubscribe()
    unsubscribeRestart()
  })

  it('does not notify a listener unsubscribed during stop notification', () => {
    const owner = start()
    let unsubscribe = () => {}
    owner.subscribe(() => unsubscribe())
    const listener = vi.fn()
    unsubscribe = owner.subscribe(listener)
    stops[0]()
    expect(listener).not.toHaveBeenCalled()
  })

  it('does nothing on visibility when healthy, pending or natively reconnecting', async () => {
    const owner = await live()
    visible()
    expect(statusRequests).toHaveLength(1)
    sources[0].options.onPayloadError(malformed)
    visible()
    visible()
    expect(statusRequests).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1_000)
    visible()
    expect(statusRequests).toHaveLength(2)
    interrupt()
    await vi.advanceTimersByTimeAsync(30_000)
    visible()
    expect(sources).toHaveLength(1)
    expect(statusRequests).toHaveLength(2)
    expect(owner.getSnapshot().synchronization).toBe('unsynchronized')
  })

  it('coalesces visibility recovery for a terminal client with an already scheduled replacement', async () => {
    start()
    interrupt(2)
    visible()
    visible()
    expect(createSSEClient).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(createSSEClient).toHaveBeenCalledTimes(2)
    visible()
    expect(createSSEClient).toHaveBeenCalledTimes(2)
  })

  it('cleans up requests, timers and listeners before stale callbacks can run', async () => {
    const remove = vi.spyOn(document, 'removeEventListener')
    const owner = start()
    const stop = stops[0]
    open()
    send(event())
    statusRequests[0].signal?.addEventListener('abort', () => {
      send(event('up', t3))
      open()
      sources[0].options.onPayloadError(malformed)
    })
    stop()
    const afterStop = owner.getSnapshot()
    await settle(0)
    interrupt()
    send(event())
    visible()
    await vi.advanceTimersByTimeAsync(120_000)
    expect(owner.getSnapshot()).toBe(afterStop)
    expect(sources[0].close).toHaveBeenCalledOnce()
    expect(statusRequests[0].signal?.aborted).toBe(true)
    expect(statusRequests).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
    expect(remove).toHaveBeenCalledWith('visibilitychange', expect.any(Function))
    stop()
    expect(sources[0].close).toHaveBeenCalledOnce()
  })

  it('supports setup/cleanup replay without allowing old cleanup to stop the new session', async () => {
    const owner = createLiveStatusCoordinator()
    const stopFirst = owner.start()
    open()
    send(event())
    stopFirst()
    const stopSecond = owner.start()
    stops.push(stopSecond)
    stopFirst()
    open(1)
    await settle(1)
    await settle(0, { services: [] })
    expect(owner.getSnapshot().synchronization).toBe('synchronized')
    expect(owner.getSnapshot().services?.[0].status.state).toBe('up')
    expect(sources[1].closed).toBe(false)
  })

  it('does not create duplicate clients on repeated start and cancels pending recovery on stop', async () => {
    const owner = await live()
    const stopAgain = owner.start()
    expect(sources).toHaveLength(1)
    sources[0].options.onPayloadError(malformed)
    stopAgain()
    await vi.advanceTimersByTimeAsync(120_000)
    expect(statusRequests).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
  })
})
