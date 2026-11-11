import { StrictMode, useSyncExternalStore } from 'react'
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { makeService } from '@/test/services'
import { useLiveStatus } from './useLiveStatus'
import { createLiveStatusCoordinator } from './coordinator'

const checkedAt = '2026-11-11T00:00:01.123456789+00:00'
const changedAt = '2026-11-11T08:00:02.123456790+08:00'
const services = [makeService('a', 'up', checkedAt)]
const emptyIncidents = { active: [], resolved: [] }

function deferredResponse() {
  let resolve!: (response: Response) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<Response>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

let sources: MockEventSource[]
let requests: (ReturnType<typeof deferredResponse> & { url: string; signal: AbortSignal | null | undefined })[]

class MockEventSource extends EventTarget {
  readyState = 0
  readonly url: string
  readonly close = vi.fn(() => { this.readyState = 2 })

  constructor(url: string) {
    super()
    this.url = url
    sources.push(this)
  }

  open() {
    this.readyState = 1
    this.dispatchEvent(new Event('open'))
  }

  error(readyState = 0) {
    this.readyState = readyState
    this.dispatchEvent(new Event('error'))
  }

  message(type: string, data: unknown) {
    this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(data) }))
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(Math, 'random').mockReturnValue(0.5)
  sources = []
  requests = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn<typeof fetch>((input, options) => {
    const pending = { ...deferredResponse(), url: String(input), signal: options?.signal }
    requests.push(pending)
    return pending.promise
  }))
})

afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
})

async function settlePair(offset = requests.length - 2) {
  await act(async () => {
    requests[offset].resolve(Response.json({ services }))
    requests[offset + 1].resolve(Response.json(emptyIncidents))
  })
}

describe('useLiveStatus transport integration', () => {
  it('updates a still-mounted external-store consumer when its coordinator stops', async () => {
    const owner = createLiveStatusCoordinator()
    const stop = owner.start()
    const { result, unmount } = renderHook(() => useSyncExternalStore(owner.subscribe, owner.getSnapshot))
    act(() => sources[0].open())
    await settlePair()
    expect(result.current.synchronization).toBe('synchronized')
    act(stop)
    expect(result.current).toBe(owner.getSnapshot())
    expect(result.current).toMatchObject({ transport: 'closed', synchronization: 'unsynchronized' })
    unmount()
    expect(sources[0].close).toHaveBeenCalledOnce()
  })

  it('subscribes to real FE-009/010 modules and publishes validated data after SSE opens', async () => {
    const { result, unmount } = renderHook(useLiveStatus)
    expect(sources).toHaveLength(1)
    expect(sources[0].url).toBe('/api/v1/events')
    expect(requests).toHaveLength(0)
    expect(result.current.services).toBeNull()
    act(() => sources[0].open())
    expect(requests.map(request => request.url)).toEqual(['/api/v1/status', '/api/v1/incidents'])
    expect(result.current.synchronization).toBe('synchronizing')
    await settlePair()
    expect(result.current.synchronization).toBe('synchronized')
    expect(result.current.services).toEqual(services)
    expect(result.current.incidents).toEqual(emptyIncidents)
    const settled = result.current
    expect(result.current).toBe(settled)
    unmount()
    expect(sources[0].close).toHaveBeenCalledOnce()
  })

  it('buffers real validated events and preserves timestamp precision and raw strings', async () => {
    const { result, unmount } = renderHook(useLiveStatus)
    act(() => sources[0].open())
    act(() => sources[0].message('status.changed', { serviceId: 'a', state: 'down', lastCheckedAt: changedAt }))
    expect(result.current.services).toBeNull()
    await settlePair()
    expect(result.current.services?.[0].status).toEqual({ state: 'down', lastCheckedAt: changedAt })
    expect(result.current.synchronization).toBe('synchronized')
    unmount()
  })

  it('exposes per-resource validation failures without fabricating empty services', async () => {
    const { result, unmount } = renderHook(useLiveStatus)
    act(() => sources[0].open())
    await act(async () => {
      requests[0].resolve(Response.json({ services: [{ id: 'invalid' }] }))
      requests[1].resolve(Response.json(emptyIncidents))
    })
    expect(result.current.services).toBeNull()
    expect(result.current.incidents).toEqual(emptyIncidents)
    expect(result.current.errors.status).toMatchObject({ kind: 'invalid-response' })
    expect(result.current.synchronization).toBe('unsynchronized')
    unmount()
  })

  it('preserves payload diagnostics and recovers using the same native source', async () => {
    const { result, unmount } = renderHook(useLiveStatus)
    act(() => sources[0].open())
    await settlePair()
    act(() => sources[0].message('status.changed', { serviceId: 'a', state: 'degraded', lastCheckedAt: changedAt }))
    expect(result.current.errors.stream).toMatchObject({ kind: 'invalid-payload' })
    expect(result.current.synchronization).toBe('unsynchronized')
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    await settlePair(2)
    expect(result.current.synchronization).toBe('synchronized')
    expect(sources).toHaveLength(1)
    unmount()
  })

  it('keeps REST-only startup degraded and stops issuing REST requests after success', async () => {
    const { result, unmount } = renderHook(useLiveStatus)
    await act(async () => { await vi.advanceTimersByTimeAsync(2_000) })
    await settlePair()
    expect(result.current.services).toEqual(services)
    expect(result.current.synchronization).toBe('unsynchronized')
    await act(async () => { await vi.advanceTimersByTimeAsync(120_000) })
    expect(requests).toHaveLength(2)
    act(() => sources[0].open())
    expect(result.current.synchronization).toBe('synchronizing')
    await settlePair(2)
    expect(result.current.synchronization).toBe('synchronized')
    unmount()
  })

  it('uses native reopening and replaces terminally closed sources without simultaneous clients', async () => {
    const { result, unmount } = renderHook(useLiveStatus)
    act(() => sources[0].open())
    await settlePair()
    act(() => sources[0].error())
    expect(result.current.transport).toBe('reconnecting')
    expect(sources[0].close).not.toHaveBeenCalled()
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    act(() => sources[0].open())
    await settlePair(2)
    expect(result.current.synchronization).toBe('synchronized')
    act(() => sources[0].error(2))
    expect(sources[0].close).toHaveBeenCalledOnce()
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    expect(sources).toHaveLength(2)
    act(() => sources[1].open())
    await settlePair(4)
    expect(result.current.synchronization).toBe('synchronized')
    unmount()
  })

  it('cleans up Strict Mode effect replay and isolates obsolete responses and callbacks', async () => {
    const { result, unmount } = renderHook(useLiveStatus, { wrapper: StrictMode })
    expect(sources).toHaveLength(2)
    expect(sources[0].close).toHaveBeenCalledOnce()
    expect(sources[1].close).not.toHaveBeenCalled()
    act(() => sources[0].open())
    expect(requests).toHaveLength(0)
    act(() => sources[1].open())
    await settlePair()
    const accepted = result.current
    act(() => {
      sources[0].message('status.changed', { serviceId: 'a', state: 'down', lastCheckedAt: changedAt })
      sources[0].error(2)
    })
    expect(result.current).toBe(accepted)
    unmount()
    expect(sources[1].close).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('aborts in-flight fetches and ignores completion after unmount', async () => {
    const { unmount } = renderHook(useLiveStatus)
    act(() => sources[0].open())
    unmount()
    expect(requests[0].signal?.aborted).toBe(true)
    expect(requests[1].signal?.aborted).toBe(true)
    await settlePair()
    await act(async () => { await vi.advanceTimersByTimeAsync(120_000) })
    expect(requests).toHaveLength(2)
    expect(sources).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('owns independent sessions for distinct mounted consumers', async () => {
    const first = renderHook(useLiveStatus)
    const second = renderHook(useLiveStatus)
    expect(sources).toHaveLength(2)
    act(() => sources[0].open())
    await settlePair()
    expect(first.result.current.synchronization).toBe('synchronized')
    expect(second.result.current.synchronization).toBe('waiting')
    first.unmount()
    expect(sources[1].close).not.toHaveBeenCalled()
    second.unmount()
  })
})
