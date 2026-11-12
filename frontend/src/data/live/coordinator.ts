import type { IncidentsResponse, StatusResponse } from '../contracts'
import { fetchIncidentsSnapshot, fetchStatusSnapshot } from './api'
import {
  createSSEClient, type LiveEvent, type SSEClient, type SSEClientOptions,
  type SSEConnectionEvent, type SSEPayloadError,
} from './events'
import type { ReconciliationIssue, ReconciliationResult, ReconciliationState } from './reconciliation'
import { createInitialReconciliationResult, reconciliationReducer } from './reducer'

type TransportStatus = 'connecting' | 'open' | 'reconnecting' | 'closed'
type SynchronizationStatus = 'waiting' | 'synchronizing' | 'unsynchronized' | 'synchronized'
type StreamFailure = SSEPayloadError | Extract<SSEConnectionEvent, { type: 'transport-error' }>
  | { kind: 'setup-error'; cause: unknown }

export interface LiveStatusErrors {
  readonly status: Error | null
  readonly incidents: Error | null
  readonly stream: StreamFailure | null
  readonly reconciliation: readonly ReconciliationIssue[]
  readonly recovery: 'buffer-overflow' | null
}

export interface LiveStatusSnapshot {
  readonly services: ReconciliationState['services']
  readonly incidents: ReconciliationState['incidents']
  readonly transport: TransportStatus
  readonly synchronization: SynchronizationStatus
  readonly errors: LiveStatusErrors
}

type Timer = ReturnType<typeof setTimeout>
type Outcome<T> = { kind: 'pending' } | { kind: 'fulfilled'; value: T } | { kind: 'rejected'; error: Error }

interface OwnedClient {
  client: SSEClient | null
}

interface Attempt {
  baseline: ReconciliationResult
  connection: OwnedClient | null
  controller: AbortController
  events: LiveEvent[]
  deadline: Timer | null
  status: Outcome<StatusResponse>
  incidents: Outcome<IncidentsResponse>
}

interface Session {
  accepted: ReconciliationResult
  transport: TransportStatus
  synchronization: SynchronizationStatus
  errors: LiveStatusErrors
  source: OwnedClient | null
  attempt: Attempt | null
  grace: Timer | null
  graceElapsed: boolean
  restStarted: boolean
  startupComplete: boolean
  recoveryTimer: Timer | null
  recoveryAt: number | null
  backoffIndex: number
  onVisibility: () => void
}

const startupGraceMs = 2_000
const synchronizationDeadlineMs = 10_000
const eventBufferCapacity = 256
const recoveryDelaysMs = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000]

function emptyErrors(): LiveStatusErrors {
  return { status: null, incidents: null, stream: null, reconciliation: [], recovery: null }
}

function requestError(cause: unknown): Error {
  return cause instanceof Error ? cause : new Error('Snapshot request failed', { cause })
}

// Construction is dormant: sockets, requests and listeners belong to start().
// This is one owner per hook instance, with no module-level mutable state.
export function createLiveStatusCoordinator() {
  const listeners = new Set<() => void>()
  let current: Session | null = null
  let snapshot: LiveStatusSnapshot = {
    services: null, incidents: null, transport: 'connecting', synchronization: 'waiting', errors: emptyErrors(),
  }

  function active(session: Session): boolean {
    return current === session
  }

  function owns(session: Session, source: OwnedClient): boolean {
    return active(session) && session.source === source
  }

  function publish(session: Session) {
    if (!active(session)) return
    const next: LiveStatusSnapshot = {
      services: session.accepted.state.services, incidents: session.accepted.state.incidents,
      transport: session.transport, synchronization: session.synchronization, errors: session.errors,
    }
    if (next.services === snapshot.services && next.incidents === snapshot.incidents
      && next.transport === snapshot.transport && next.synchronization === snapshot.synchronization
      && next.errors === snapshot.errors) return
    snapshot = next
    for (const listener of [...listeners]) {
      if (!active(session)) break
      listener()
    }
  }

  function rememberIssues(session: Session) {
    if (!session.accepted.successful) {
      session.errors = { ...session.errors, reconciliation: session.accepted.issues }
    }
  }

  function cancelRecoveryTimer(session: Session) {
    if (session.recoveryTimer !== null) clearTimeout(session.recoveryTimer)
    session.recoveryTimer = null
  }

  function requestRecovery(session: Session) {
    if (!active(session)) return
    session.synchronization = session.attempt ? 'synchronizing' : 'unsynchronized'
    if (session.recoveryAt === null) {
      const base = recoveryDelaysMs[session.backoffIndex]
      session.backoffIndex = Math.min(session.backoffIndex + 1, recoveryDelaysMs.length - 1)
      session.recoveryAt = Date.now() + Math.round(Math.min(30_000, base * (0.8 + Math.random() * 0.4)))
    }
    // Never start work reentrantly inside abort/close/error callbacks.
    if (session.recoveryTimer === null) {
      session.recoveryTimer = setTimeout(() => {
        session.recoveryTimer = null
        recover(session)
      }, Math.max(0, session.recoveryAt - Date.now()))
    }
  }

  function takeAttempt(session: Session): Attempt | null {
    const attempt = session.attempt
    // Retire identity AND event routing before reduction, abort or notification.
    session.attempt = null
    if (attempt?.deadline !== null && attempt?.deadline !== undefined) clearTimeout(attempt.deadline)
    return attempt
  }

  function invalidateAttempt(session: Session, overflowEvent?: LiveEvent) {
    const attempt = takeAttempt(session)
    if (!attempt) return
    const events = attempt.events
    attempt.events = []
    session.synchronization = 'unsynchronized'
    // FE-011 is synchronous and invokes no external callbacks. Commit the replay
    // before abort(), which can synchronously deliver more events via listeners.
    session.accepted = reconciliationReducer(attempt.baseline, { type: 'snapshots', snapshots: {}, events })
    rememberIssues(session)
    if (overflowEvent) {
      session.accepted = reconciliationReducer(session.accepted, { type: 'event', event: overflowEvent })
      rememberIssues(session)
    }
    publish(session)
    attempt.controller.abort()
  }

  function finishAttempt(session: Session, attempt: Attempt, timedOut = false) {
    if (!active(session) || session.attempt !== attempt) return
    if (timedOut) {
      const error = new DOMException('Snapshot synchronization timed out', 'TimeoutError')
      if (attempt.status.kind === 'pending') attempt.status = { kind: 'rejected', error }
      if (attempt.incidents.kind === 'pending') attempt.incidents = { kind: 'rejected', error }
    }
    if (attempt.status.kind === 'pending' || attempt.incidents.kind === 'pending') return
    takeAttempt(session)
    const events = attempt.events
    attempt.events = []
    session.accepted = reconciliationReducer(attempt.baseline, {
      type: 'snapshots',
      snapshots: {
        status: attempt.status.kind === 'fulfilled' ? attempt.status.value : undefined,
        incidents: attempt.incidents.kind === 'fulfilled' ? attempt.incidents.value : undefined,
      },
      events,
    })
    session.errors = {
      ...session.errors,
      status: attempt.status.kind === 'rejected' ? attempt.status.error : null,
      incidents: attempt.incidents.kind === 'rejected' ? attempt.incidents.error : null,
    }
    rememberIssues(session)
    const complete = attempt.status.kind === 'fulfilled' && attempt.incidents.kind === 'fulfilled'
      && session.accepted.successful
    if (complete) {
      session.startupComplete = true
      if (session.grace !== null) clearTimeout(session.grace)
      session.grace = null
    }
    const synchronized = complete && attempt.connection !== null
      && session.source === attempt.connection && session.transport === 'open'
    session.synchronization = synchronized ? 'synchronized' : 'unsynchronized'
    if (synchronized) {
      session.errors = emptyErrors()
      session.backoffIndex = 0
      session.recoveryAt = null
      cancelRecoveryTimer(session)
    }
    if (!complete) requestRecovery(session)
    // State, health and routing are committed before external callbacks run.
    publish(session)
    if (timedOut) attempt.controller.abort()
  }

  function beginAttempt(session: Session) {
    if (!active(session) || session.attempt) return
    session.restStarted = true
    const attempt: Attempt = {
      baseline: session.accepted,
      connection: session.transport === 'open' ? session.source : null,
      controller: new AbortController(), events: [], deadline: null,
      status: { kind: 'pending' }, incidents: { kind: 'pending' },
    }
    session.attempt = attempt
    session.synchronization = 'synchronizing'
    attempt.deadline = setTimeout(() => finishAttempt(session, attempt, true), synchronizationDeadlineMs)
    const signal = attempt.controller.signal
    // Install the barrier before invoking either transport. Outcomes remain
    // private until the pair settles or the owner deadline expires.
    void fetchStatusSnapshot(signal).then(value => {
      if (!active(session) || session.attempt !== attempt) return
      attempt.status = { kind: 'fulfilled', value }
      finishAttempt(session, attempt)
    }, cause => {
      if (!active(session) || session.attempt !== attempt) return
      attempt.status = { kind: 'rejected', error: requestError(cause) }
      finishAttempt(session, attempt)
    })
    if (!active(session) || session.attempt !== attempt) return
    void fetchIncidentsSnapshot(signal).then(value => {
      if (!active(session) || session.attempt !== attempt) return
      attempt.incidents = { kind: 'fulfilled', value }
      finishAttempt(session, attempt)
    }, cause => {
      if (!active(session) || session.attempt !== attempt) return
      attempt.incidents = { kind: 'rejected', error: requestError(cause) }
      finishAttempt(session, attempt)
    })
    publish(session)
  }

  function onEvent(session: Session, source: OwnedClient, event: LiveEvent) {
    if (!owns(session, source) || session.transport !== 'open') return
    if (session.attempt) {
      if (session.attempt.events.length < eventBufferCapacity) {
        session.attempt.events.push(event)
        return
      }
      session.errors = { ...session.errors, recovery: 'buffer-overflow' }
      requestRecovery(session)
      invalidateAttempt(session, event)
    } else {
      session.accepted = reconciliationReducer(session.accepted, { type: 'event', event })
      rememberIssues(session)
      if (!session.accepted.successful) requestRecovery(session)
    }
    publish(session)
  }

  function onConnection(session: Session, source: OwnedClient, event: SSEConnectionEvent) {
    if (!owns(session, source)) return
    if (event.type === 'open') {
      if (session.transport === 'open') return
      session.transport = 'open'
      session.synchronization = session.attempt && !session.attempt.connection ? 'synchronizing' : 'unsynchronized'
      // A fallback cannot prove continuity with this newly opened stream.
      invalidateAttempt(session)
      recover(session)
    } else {
      const wasOpen = session.transport === 'open'
      const terminal = event.type === 'closed' || event.readyState === 2
      session.transport = terminal ? 'closed' : 'reconnecting'
      session.synchronization = session.attempt && !session.attempt.connection ? 'synchronizing' : 'unsynchronized'
      session.errors = {
        ...session.errors,
        stream: event.type === 'closed' ? { type: 'transport-error', readyState: 2 } : event,
      }
      if (terminal) {
        // Retire the source before abort/close can deliver its callbacks again.
        session.source = null
      }
      if (terminal || wasOpen) requestRecovery(session)
      // A REST-only attempt never had a stream guarantee to lose. Repeated
      // native startup failures must not starve its independent REST requests.
      if (session.attempt?.connection) invalidateAttempt(session)
      if (terminal) source.client?.close()
    }
    publish(session)
  }

  function connect(session: Session) {
    if (!active(session) || session.source) return
    const source: OwnedClient = { client: null }
    session.source = source
    session.transport = 'connecting'
    const options: SSEClientOptions = {
      onEvent: event => onEvent(session, source, event),
      onConnection: event => onConnection(session, source, event),
      onPayloadError: error => {
        if (!owns(session, source)) return
        session.errors = { ...session.errors, stream: error }
        requestRecovery(session)
        invalidateAttempt(session)
        publish(session)
      },
    }
    try {
      const client = createSSEClient(options)
      if (owns(session, source)) source.client = client
      else client.close()
    } catch (cause) {
      if (!owns(session, source)) return
      session.source = null
      session.transport = 'closed'
      session.errors = { ...session.errors, stream: { kind: 'setup-error', cause } }
      requestRecovery(session)
    }
    publish(session)
  }

  function recover(session: Session) {
    if (!active(session) || session.synchronization === 'synchronized') return
    if (session.recoveryAt !== null && session.recoveryAt > Date.now()) {
      if (session.recoveryTimer === null) {
        session.recoveryTimer = setTimeout(() => {
          session.recoveryTimer = null
          recover(session)
        }, session.recoveryAt - Date.now())
      }
      return
    }
    const replace = session.source === null && session.transport === 'closed'
    const fallback = session.graceElapsed && !session.startupComplete
    if (!replace && (session.attempt || (session.transport !== 'open' && !fallback))) return
    session.recoveryAt = null
    cancelRecoveryTimer(session)
    if (replace) connect(session)
    if (!active(session) || session.attempt) return
    // A failed EventSource construction must not starve independent startup
    // data loading. Its newly scheduled replacement can run alongside fallback.
    if (fallback || (session.transport === 'open' && session.recoveryAt === null)) beginAttempt(session)
  }

  function stop(session: Session) {
    if (!active(session)) return
    current = null
    const attempt = takeAttempt(session)
    const source = session.source
    session.source = null
    if (session.grace !== null) clearTimeout(session.grace)
    cancelRecoveryTimer(session)
    document.removeEventListener('visibilitychange', session.onVisibility)
    const stoppedSnapshot: LiveStatusSnapshot = { ...snapshot, transport: 'closed', synchronization: 'unsynchronized' }
    snapshot = stoppedSnapshot
    if (attempt) {
      attempt.events = []
      attempt.controller.abort()
    }
    source?.client?.close()
    // Retained subscribers must observe the stopped state. Finish resource
    // cleanup first, and stop this notification pass if a listener restarts us.
    for (const listener of [...listeners]) {
      if (current !== null || snapshot !== stoppedSnapshot) break
      if (listeners.has(listener)) listener()
    }
  }

  function start(): () => void {
    if (current) {
      const session = current
      return () => stop(session)
    }
    const session: Session = {
      accepted: createInitialReconciliationResult(), transport: 'connecting', synchronization: 'waiting',
      errors: emptyErrors(), source: null, attempt: null, grace: null,
      graceElapsed: false, restStarted: false, startupComplete: false,
      recoveryTimer: null, recoveryAt: null, backoffIndex: 0,
      onVisibility: () => {
        if (document.visibilityState === 'visible') recover(session)
      },
    }
    current = session
    document.addEventListener('visibilitychange', session.onVisibility)
    session.grace = setTimeout(() => {
      if (!active(session)) return
      session.grace = null
      session.graceElapsed = true
      if (!session.restStarted && session.transport !== 'open') beginAttempt(session)
      else recover(session)
    }, startupGraceMs)
    connect(session)
    return () => stop(session)
  }

  return {
    getSnapshot: () => snapshot,
    subscribe(listener: () => void) {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    start,
  }
}
