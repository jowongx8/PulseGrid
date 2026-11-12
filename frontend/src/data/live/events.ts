import { apiPaths } from '@/config/api'
import type { IncidentOpenedEventPayload, IncidentResolvedEventPayload, StatusChangedEventPayload } from '../contracts'
import { validateIncidentOpenedEventPayload, validateIncidentResolvedEventPayload, validateStatusChangedEventPayload } from './validation'

export type LiveEvent =
  | { type: 'status.changed'; payload: StatusChangedEventPayload }
  | { type: 'incident.opened'; payload: IncidentOpenedEventPayload }
  | { type: 'incident.resolved'; payload: IncidentResolvedEventPayload }

export type SSEConnectionEvent =
  | { type: 'open'; reconnected: boolean }
  | { type: 'transport-error'; readyState: number }
  | { type: 'closed' }

export interface SSEPayloadError {
  eventType: LiveEvent['type']
  kind: 'invalid-json' | 'invalid-payload'
  cause: unknown
}

export interface SSEClientOptions {
  onEvent: (event: LiveEvent) => void
  onConnection: (event: SSEConnectionEvent) => void
  onPayloadError: (error: SSEPayloadError) => void
}

export interface SSEClient {
  close(): void
}

export function createSSEClient(options: SSEClientOptions): SSEClient {
  const source = new EventSource(apiPaths.events)
  let disposed = false
  let opened = false

  function readPayload<T>(type: LiveEvent['type'], event: Event, validate: (value: unknown) => asserts value is T): T | undefined {
    if (disposed) return
    let payload: unknown
    try {
      payload = JSON.parse((event as MessageEvent<string>).data)
    } catch (cause) {
      options.onPayloadError({ eventType: type, kind: 'invalid-json', cause })
      return
    }
    try {
      validate(payload)
    } catch (cause) {
      options.onPayloadError({ eventType: type, kind: 'invalid-payload', cause })
      return
    }
    return payload
  }

  function onStatusChanged(event: Event) {
    const payload = readPayload('status.changed', event, validateStatusChangedEventPayload)
    if (payload !== undefined) options.onEvent({ type: 'status.changed', payload })
  }

  function onIncidentOpened(event: Event) {
    const payload = readPayload('incident.opened', event, validateIncidentOpenedEventPayload)
    if (payload !== undefined) options.onEvent({ type: 'incident.opened', payload })
  }

  function onIncidentResolved(event: Event) {
    const payload = readPayload('incident.resolved', event, validateIncidentResolvedEventPayload)
    if (payload !== undefined) options.onEvent({ type: 'incident.resolved', payload })
  }

  function onOpen(event: Event) {
    // Named SSE data uses MessageEvent, even if an unsupported name is "open".
    if (disposed || event instanceof MessageEvent) return
    const reconnected = opened
    opened = true
    options.onConnection({ type: 'open', reconnected })
  }

  function onError(event: Event) {
    if (disposed || event instanceof MessageEvent) return
    // CLOSED means this native instance stopped retrying, not that the server
    // can never recover. The owner may dispose it and create a replacement.
    options.onConnection({ type: 'transport-error', readyState: source.readyState })
  }

  source.addEventListener('open', onOpen)
  source.addEventListener('error', onError)
  source.addEventListener('status.changed', onStatusChanged)
  source.addEventListener('incident.opened', onIncidentOpened)
  source.addEventListener('incident.resolved', onIncidentResolved)

  return {
    close() {
      if (disposed) return
      disposed = true
      source.removeEventListener('open', onOpen)
      source.removeEventListener('error', onError)
      source.removeEventListener('status.changed', onStatusChanged)
      source.removeEventListener('incident.opened', onIncidentOpened)
      source.removeEventListener('incident.resolved', onIncidentResolved)
      source.close()
      options.onConnection({ type: 'closed' })
    },
  }
}
