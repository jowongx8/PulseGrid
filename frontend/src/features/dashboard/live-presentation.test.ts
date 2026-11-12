import { describe, expect, it } from 'vitest'
import type { LiveStatusErrors, LiveStatusSnapshot } from '@/data/live/coordinator'
import { makeService } from '@/test/services'
import { deriveConnectionStatus, hasResourceFailure } from './live-presentation'

function snapshot(overrides: Partial<LiveStatusSnapshot> = {}, errors: Partial<LiveStatusErrors> = {}): LiveStatusSnapshot {
  return {
    services: null, incidents: null, transport: 'connecting', synchronization: 'waiting', ...overrides,
    errors: { status: null, incidents: null, stream: null, reconciliation: [], recovery: null, ...errors },
  }
}

describe('connection labels', () => {
  it('starts Connecting and keeps ordinary native retries Connecting before data or resource failure', () => {
    expect(deriveConnectionStatus(snapshot(), false)).toBe('connecting')
    expect(deriveConnectionStatus(snapshot({ transport: 'reconnecting', synchronization: 'unsynchronized' }, { stream: { type: 'transport-error', readyState: 0 } }), false)).toBe('connecting')
    expect(deriveConnectionStatus(snapshot({ transport: 'open', synchronization: 'synchronizing' }, { stream: { type: 'transport-error', readyState: 0 } }), false)).toBe('connecting')
  })

  it.each([null, [], [makeService('a', 'up')]])('requires open, synchronized transport for Live regardless of services %j', services => {
    expect(deriveConnectionStatus(snapshot({ services, transport: 'open', synchronization: 'synchronized' }), false)).toBe('live')
    expect(deriveConnectionStatus(snapshot({ services, transport: 'open', synchronization: 'synchronizing' }), false)).toBe('connecting')
    expect(deriveConnectionStatus(snapshot({ services, transport: 'closed', synchronization: 'synchronized' }), true)).toBe('offline')
  })

  it.each(['connecting', 'reconnecting'] as const)('shows Offline for initial accepted REST-only data over %s transport', transport => {
    expect(deriveConnectionStatus(snapshot({ transport, services: [] }), false)).toBe('offline')
    expect(deriveConnectionStatus(snapshot({ transport, incidents: { active: [], resolved: [] } }), false)).toBe('offline')
    expect(deriveConnectionStatus(snapshot({ transport }, { status: new Error('Unavailable') }), false)).toBe('offline')
  })

  it.each(['connecting', 'reconnecting', 'open'] as const)('shows Reconnecting after prior Live over %s transport', transport => {
    expect(deriveConnectionStatus(snapshot({ transport, synchronization: 'synchronizing' }), true)).toBe('reconnecting')
    expect(deriveConnectionStatus(snapshot({ transport, synchronization: 'unsynchronized' }), true)).toBe('reconnecting')
  })

  it('shows terminal closure as Offline even after previous Live', () => {
    expect(deriveConnectionStatus(snapshot({ transport: 'closed' }), false)).toBe('offline')
    expect(deriveConnectionStatus(snapshot({ transport: 'closed' }), true)).toBe('offline')
  })

  it('shows open-stream synchronization failures as Reconnecting before first Live', () => {
    const pending = { transport: 'open', synchronization: 'synchronizing' } as const
    expect(deriveConnectionStatus(snapshot({ transport: 'open', synchronization: 'unsynchronized' }), false)).toBe('reconnecting')
    expect(deriveConnectionStatus(snapshot(pending, { incidents: new Error('Unavailable') }), false)).toBe('reconnecting')
    expect(deriveConnectionStatus(snapshot(pending, { recovery: 'buffer-overflow' }), false)).toBe('reconnecting')
    expect(deriveConnectionStatus(snapshot(pending, { stream: { kind: 'invalid-payload', eventType: 'status.changed', cause: new Error('Invalid') } }), false)).toBe('reconnecting')
    expect(deriveConnectionStatus(snapshot(pending, { reconciliation: [{ kind: 'status-conflict', resource: 'status', message: 'Conflict' }] }), false)).toBe('reconnecting')
    expect(deriveConnectionStatus(snapshot(pending), false, true)).toBe('reconnecting')
    expect(deriveConnectionStatus(snapshot({ transport: 'reconnecting' }), false, true)).toBe('offline')
  })
})

describe('resource failures', () => {
  it.each(['status', 'incidents'] as const)('does not infer %s failure from stream errors or overall health', resource => {
    expect(hasResourceFailure(snapshot({ transport: 'closed', synchronization: 'unsynchronized' }, {
      stream: { type: 'transport-error', readyState: 2 }, recovery: 'buffer-overflow',
    }), resource)).toBe(false)
    expect(hasResourceFailure(snapshot({}, { stream: { kind: 'setup-error', cause: new Error('Unavailable') } }), resource)).toBe(false)
    expect(hasResourceFailure(snapshot({}, { stream: { kind: 'invalid-json', eventType: 'status.changed', cause: new Error('Invalid') } }), resource)).toBe(false)
  })

  it.each(['status', 'incidents'] as const)('recognizes only %s-specific request and reconciliation failures', resource => {
    const other = resource === 'status' ? 'incidents' : 'status'
    expect(hasResourceFailure(snapshot({}, { [resource]: new Error('Unavailable') }), resource)).toBe(true)
    expect(hasResourceFailure(snapshot({}, { [other]: new Error('Unavailable') }), resource)).toBe(false)
    expect(hasResourceFailure(snapshot({}, { reconciliation: [{ kind: 'missing-baseline', resource, message: 'Missing baseline' }] }), resource)).toBe(true)
    expect(hasResourceFailure(snapshot({}, { reconciliation: [{ kind: 'missing-baseline', resource: other, message: 'Missing baseline' }] }), resource)).toBe(false)
  })
})
