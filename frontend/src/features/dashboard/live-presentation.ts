import type { LiveStatusSnapshot } from '@/data/live/coordinator'

export type ConnectionStatus = 'connecting' | 'live' | 'reconnecting' | 'offline'

export function hasResourceFailure(snapshot: LiveStatusSnapshot, resource: 'status' | 'incidents'): boolean {
  return snapshot.errors[resource] !== null
    || snapshot.errors.reconciliation.some(issue => issue.resource === resource)
}

export function deriveConnectionStatus(snapshot: LiveStatusSnapshot, hasBeenLive: boolean, resourceUnavailable = false): ConnectionStatus {
  if (snapshot.transport === 'closed') return 'offline'
  if (snapshot.transport === 'open' && snapshot.synchronization === 'synchronized') return 'live'
  if (hasBeenLive) return 'reconnecting'

  const resourceFailure = resourceUnavailable || hasResourceFailure(snapshot, 'status') || hasResourceFailure(snapshot, 'incidents')
  if (snapshot.transport === 'open') {
    const payloadFailure = snapshot.errors.stream !== null && 'kind' in snapshot.errors.stream
      && (snapshot.errors.stream.kind === 'invalid-json' || snapshot.errors.stream.kind === 'invalid-payload')
    return snapshot.synchronization === 'unsynchronized' || resourceFailure || payloadFailure || snapshot.errors.recovery !== null
      ? 'reconnecting' : 'connecting'
  }
  return snapshot.services !== null || snapshot.incidents !== null || resourceFailure ? 'offline' : 'connecting'
}
