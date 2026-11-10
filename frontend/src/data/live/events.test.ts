import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SSEClient, SSEClientOptions } from './events'
import { createSSEClient } from './events'

const status = { serviceId: 'github', state: 'down', lastCheckedAt: '2026-09-22T15:00:00.123456789+10:00' }
const opened = { serviceId: 'github', startedAt: '2026-09-22T05:00:00.123456Z', resolvedAt: null }
const resolved = { ...opened, resolvedAt: '2026-09-22T05:08:00.654321Z' }
let sources: MockEventSource[]
let clients: SSEClient[]

class MockEventSource extends EventTarget {
  readyState = 0
  readonly url: string
  readonly options: EventSourceInit | undefined
  readonly add = vi.spyOn(this as EventTarget, 'addEventListener')
  readonly remove = vi.spyOn(this as EventTarget, 'removeEventListener')
  readonly close = vi.fn(() => { this.readyState = 2 })

  constructor(url: string, options?: EventSourceInit) {
    super()
    this.url = url
    this.options = options
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

  message(type: string, data: string) {
    this.dispatchEvent(new MessageEvent(type, { data }))
  }

  listener(type: string) {
    const listener = this.add.mock.calls.find(([name]) => name === type)?.[1]
    if (typeof listener !== 'function') throw new Error(`Missing listener for ${type}`)
    return listener
  }
}

beforeEach(() => {
  sources = []
  clients = []
  vi.stubGlobal('EventSource', MockEventSource)
})

afterEach(() => { for (const client of clients) client.close() })

function start(overrides: Partial<SSEClientOptions> = {}) {
  const callbacks = {
    onEvent: vi.fn<SSEClientOptions['onEvent']>(overrides.onEvent),
    onConnection: vi.fn<SSEClientOptions['onConnection']>(overrides.onConnection),
    onPayloadError: vi.fn<SSEClientOptions['onPayloadError']>(overrides.onPayloadError),
  }
  const client = createSSEClient(callbacks)
  clients.push(client)
  return { client, source: sources[sources.length - 1], ...callbacks }
}

describe('SSE lifecycle', () => {
  it('opens one native same-origin source and registers only the supported listeners', () => {
    const { source, onConnection } = start()
    expect(sources).toHaveLength(1)
    expect(source.url).toBe('/api/v1/events')
    expect(source.options).toBeUndefined()
    expect(source.add.mock.calls.map(([name]) => name)).toEqual(['open', 'error', 'status.changed', 'incident.opened', 'incident.resolved'])
    expect(onConnection).not.toHaveBeenCalled()
  })

  it('reports first open, repeated interruptions and native reopening without replacing the source', () => {
    const { source, onConnection } = start()
    source.error()
    source.open()
    source.error()
    source.error()
    source.open()
    expect(onConnection.mock.calls.map(([event]) => event)).toEqual([
      { type: 'transport-error', readyState: 0 },
      { type: 'open', reconnected: false },
      { type: 'transport-error', readyState: 0 },
      { type: 'transport-error', readyState: 0 },
      { type: 'open', reconnected: true },
    ])
    expect(sources).toHaveLength(1)
    expect(source.close).not.toHaveBeenCalled()
  })

  it('exposes terminal CLOSED and lets the owner dispose and replace the client', () => {
    const first = start()
    first.source.open()
    first.source.error(2)
    expect(first.onConnection).toHaveBeenLastCalledWith({ type: 'transport-error', readyState: 2 })
    expect(first.source.close).not.toHaveBeenCalled()
    expect(sources).toHaveLength(1)
    first.client.close()
    const replacement = start()
    replacement.source.open()
    expect(sources).toHaveLength(2)
    expect(replacement.onConnection).toHaveBeenLastCalledWith({ type: 'open', reconnected: false })
    expect(first.source.close).toHaveBeenCalledOnce()
  })

  it('cleans up once before announcing intentional closure, including reentrant close', () => {
    const onConnection = vi.fn()
    const { source, client } = start({ onConnection })
    onConnection.mockImplementation((event) => {
      if (event.type !== 'closed') return
      expect(source.close).toHaveBeenCalledOnce()
      for (const [name, listener] of source.add.mock.calls) expect(source.remove).toHaveBeenCalledWith(name, listener)
      client.close()
    })
    client.close()
    client.close()
    expect(source.close).toHaveBeenCalledOnce()
    expect(source.remove).toHaveBeenCalledTimes(5)
    expect(onConnection).toHaveBeenCalledExactlyOnceWith({ type: 'closed' })
  })

  it('suppresses queued or manually dispatched callbacks after closing before first open', () => {
    const { source, client, onEvent, onConnection, onPayloadError } = start()
    const queued = source.add.mock.calls.map(([name]) => ({ name, listener: source.listener(name) }))
    client.close()
    for (const { name, listener } of queued) {
      listener.call(source, name === 'open' || name === 'error' ? new Event(name) : new MessageEvent(name, { data: 'not JSON' }))
    }
    source.message('status.changed', JSON.stringify(status))
    expect(onEvent).not.toHaveBeenCalled()
    expect(onPayloadError).not.toHaveBeenCalled()
    expect(onConnection).toHaveBeenCalledExactlyOnceWith({ type: 'closed' })
  })

  it('does not leak listeners or connections across repeated creation and disposal', () => {
    for (let index = 0; index < 20; index += 1) {
      const { source, client } = start()
      source.open()
      client.close()
      for (const [name, listener] of source.add.mock.calls) expect(source.remove).toHaveBeenCalledWith(name, listener)
      expect(source.close).toHaveBeenCalledOnce()
    }
    expect(sources).toHaveLength(20)
  })

  it('propagates native constructor exceptions without fabricated lifecycle notifications', () => {
    const cause = new DOMException('Invalid source URL', 'SyntaxError')
    vi.stubGlobal('EventSource', vi.fn(function () { throw cause }))
    const callbacks = { onEvent: vi.fn(), onConnection: vi.fn(), onPayloadError: vi.fn() }
    expect(() => createSSEClient(callbacks)).toThrow(cause)
    expect(callbacks.onConnection).not.toHaveBeenCalled()
    expect(callbacks.onPayloadError).not.toHaveBeenCalled()
  })
})

describe('SSE event delivery', () => {
  it.each([
    ['status.changed', status], ['incident.opened', opened], ['incident.resolved', resolved],
  ])('delivers a validated %s payload with its original fields', (type, payload) => {
    const { source, onEvent, onPayloadError } = start()
    source.open()
    source.message(type, JSON.stringify(payload))
    expect(onEvent).toHaveBeenCalledExactlyOnceWith({ type, payload })
    expect(onPayloadError).not.toHaveBeenCalled()
  })

  it('delivers synchronously in receipt order without buffering, sorting or rejecting duplicates', () => {
    const { source, onEvent } = start()
    const earlier = { ...status, lastCheckedAt: '2026-09-22T15:00:00.123456788+10:00' }
    for (const [type, payload] of [['incident.resolved', resolved], ['status.changed', status], ['status.changed', earlier], ['incident.opened', opened], ['incident.opened', opened]] as const) {
      const count = onEvent.mock.calls.length
      source.message(type, JSON.stringify(payload))
      expect(onEvent).toHaveBeenCalledTimes(count + 1)
    }
    expect(onEvent.mock.calls.map(([event]) => event.type)).toEqual(['incident.resolved', 'status.changed', 'status.changed', 'incident.opened', 'incident.opened'])
    expect(onEvent).toHaveBeenNthCalledWith(3, { type: 'status.changed', payload: earlier })
    expect(onEvent).toHaveBeenNthCalledWith(4, { type: 'incident.opened', payload: opened })
  })

  it('retains additional properties without letting payload fields override the event name', () => {
    const { source, onEvent } = start()
    const payload = { ...status, type: 'incident.opened', futureProperty: true }
    source.message('status.changed', JSON.stringify(payload))
    expect(onEvent).toHaveBeenCalledExactlyOnceWith({ type: 'status.changed', payload })
  })

  it.each(['message', 'unknown.event', 'heartbeat', 'open', 'error'])('ignores unsupported/default data events: %s', (type) => {
    const { source, onEvent, onConnection, onPayloadError } = start()
    source.message(type, JSON.stringify(status))
    expect(onEvent).not.toHaveBeenCalled()
    expect(onConnection).not.toHaveBeenCalled()
    expect(onPayloadError).not.toHaveBeenCalled()
  })

  it('reports invalid JSON and invalid payloads distinctly and continues on the same stream', () => {
    const { source, onEvent, onConnection, onPayloadError } = start()
    source.open()
    source.message('incident.resolved', '{')
    source.message('status.changed', JSON.stringify({ ...status, lastCheckedAt: null }))
    expect(onPayloadError.mock.calls.map(([error]) => ({ eventType: error.eventType, kind: error.kind }))).toEqual([
      { eventType: 'incident.resolved', kind: 'invalid-json' },
      { eventType: 'status.changed', kind: 'invalid-payload' },
    ])
    expect(onPayloadError.mock.calls[0][0].cause).toBeInstanceOf(SyntaxError)
    expect(onPayloadError.mock.calls[1][0].cause).toMatchObject({ message: expect.stringContaining('payload.lastCheckedAt') })
    expect(onEvent).not.toHaveBeenCalled()
    source.error()
    source.open()
    source.message('incident.resolved', JSON.stringify(resolved))
    expect(onEvent).toHaveBeenCalledExactlyOnceWith({ type: 'incident.resolved', payload: resolved })
    expect(onConnection).toHaveBeenLastCalledWith({ type: 'open', reconnected: true })
    expect(source.close).not.toHaveBeenCalled()
  })

  it('allows closing from a delivery callback without delivering later messages', () => {
    const onEvent = vi.fn()
    const { source, client } = start({ onEvent })
    onEvent.mockImplementation(() => client.close())
    source.message('status.changed', JSON.stringify(status))
    source.message('incident.opened', JSON.stringify(opened))
    expect(onEvent).toHaveBeenCalledOnce()
    expect(source.close).toHaveBeenCalledOnce()
  })
})

describe('consumer exceptions', () => {
  // Invoke the captured listener directly so jsdom does not report the deliberate
  // exception as an unhandled browser error. Native EventTarget owns that reporting.
  it('does not misclassify exceptions from onEvent as invalid backend payloads', () => {
    const cause = new TypeError('Consumer failed')
    const onEvent = vi.fn().mockImplementationOnce(() => { throw cause })
    const { source, onPayloadError } = start({ onEvent })
    const listener = source.listener('status.changed')
    expect(() => listener.call(source, new MessageEvent('status.changed', { data: JSON.stringify(status) }))).toThrow(cause)
    expect(onPayloadError).not.toHaveBeenCalled()
    expect(source.close).not.toHaveBeenCalled()
    source.message('status.changed', JSON.stringify(status))
    expect(onEvent).toHaveBeenCalledTimes(2)
  })

  it.each(['{', JSON.stringify({ ...status, state: 'degraded' })])('does not wrap exceptions thrown by onPayloadError: %s', (data) => {
    const cause = new Error('Consumer failure reporter failed')
    const onPayloadError = vi.fn(() => { throw cause })
    const { source, onEvent } = start({ onPayloadError })
    expect(() => source.listener('status.changed').call(source, new MessageEvent('status.changed', { data }))).toThrow(cause)
    expect(onPayloadError).toHaveBeenCalledOnce()
    expect(onEvent).not.toHaveBeenCalled()
  })

  it.each(['open', 'error'])('does not wrap exceptions thrown by onConnection for %s', (type) => {
    const cause = new Error('Consumer lifecycle handler failed')
    const onConnection = vi.fn().mockImplementationOnce(() => { throw cause })
    const { source, onPayloadError } = start({ onConnection })
    expect(() => source.listener(type).call(source, new Event(type))).toThrow(cause)
    expect(onPayloadError).not.toHaveBeenCalled()
  })

  it('finishes cleanup before a close notification throws', () => {
    const cause = new Error('Consumer close handler failed')
    const { source, client } = start({ onConnection: () => { throw cause } })
    expect(() => client.close()).toThrow(cause)
    expect(source.close).toHaveBeenCalledOnce()
    expect(source.remove).toHaveBeenCalledTimes(5)
    expect(() => client.close()).not.toThrow()
  })
})
