import type {
  ApiTimestamp, CurrentStatusDTO, IncidentDTO, IncidentResolvedEventPayload,
  IncidentsResponse, ServiceDTO, StatusResponse,
} from '../contracts'
import { compareTimestamps } from '../timestamps'
import type { LiveEvent } from './events'

interface IncidentLifecycleEvidence {
  readonly closedThrough: ApiTimestamp
  readonly latestResolution: IncidentResolvedEventPayload | null
}

export interface ReconciliationState {
  readonly services: readonly ServiceDTO[] | null
  readonly incidents: IncidentsResponse | null
  // Bounded lifecycle evidence, not a permanent incident audit log.
  readonly lifecycle: ReadonlyMap<string, IncidentLifecycleEvidence>
}

export interface ReconciliationIssue {
  readonly kind: 'missing-baseline' | 'unknown-service' | 'duplicate-service'
    | 'status-conflict' | 'duplicate-incident' | 'incident-conflict' | 'resolution-conflict'
  readonly resource: 'status' | 'incidents'
  readonly serviceId?: string
  readonly incidentKey?: string
  readonly message: string
}

export interface ReconciliationResult {
  readonly state: ReconciliationState
  // Describes this operation only; it is not a connection/synchronization state.
  readonly successful: boolean
  readonly issues: readonly ReconciliationIssue[]
}

export interface ReconciliationSnapshots {
  readonly status?: StatusResponse
  readonly incidents?: IncidentsResponse
}

const incidentFeedTarget = 20

export function createInitialReconciliationState(): ReconciliationState {
  return { services: null, incidents: null, lifecycle: new Map() }
}

function incidentKey(incident: IncidentDTO): string {
  return `${incident.serviceId}|${incident.startedAt}`
}

function incidentIssue(
  kind: ReconciliationIssue['kind'], incident: IncidentDTO, message: string,
): ReconciliationIssue {
  return { kind, resource: 'incidents', serviceId: incident.serviceId, incidentKey: incidentKey(incident), message }
}

function selectStatus(
  accepted: CurrentStatusDTO, incoming: CurrentStatusDTO, serviceId: string, issues: ReconciliationIssue[],
): CurrentStatusDTO {
  const order = incoming.lastCheckedAt === null
    ? (accepted.lastCheckedAt === null ? 0 : -1)
    : (accepted.lastCheckedAt === null ? 1 : compareTimestamps(incoming.lastCheckedAt, accepted.lastCheckedAt))
  if (order > 0) return incoming
  if (order === 0 && accepted.state !== incoming.state) {
    issues.push({ kind: 'status-conflict', resource: 'status', serviceId, message: 'Equal observation times have different states.' })
  }
  return accepted
}

function sameReferences<T>(left: readonly T[], right: readonly T[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index])
}

function sameMetadata(left: ServiceDTO, right: ServiceDTO): boolean {
  return left.id === right.id && left.name === right.name && left.category === right.category
    && left.websiteUrl === right.websiteUrl && left.weight === right.weight
}

function acceptStatusSnapshot(
  previous: ReconciliationState, snapshot: StatusResponse, issues: ReconciliationIssue[],
): ReconciliationState {
  const startIssues = issues.length
  const acceptedById = new Map(previous.services?.map(service => [service.id, service]))
  const seen = new Set<string>()
  const services = snapshot.services.map(incoming => {
    if (seen.has(incoming.id)) {
      issues.push({ kind: 'duplicate-service', resource: 'status', serviceId: incoming.id, message: 'The catalogue contains a duplicate service ID.' })
    }
    seen.add(incoming.id)
    const accepted = acceptedById.get(incoming.id)
    if (!accepted) return incoming
    const status = selectStatus(accepted.status, incoming.status, incoming.id, issues)
    if (status === accepted.status && sameMetadata(accepted, incoming)) return accepted
    return status === incoming.status ? incoming : { ...incoming, status }
  })
  if (issues.length !== startIssues) return previous
  if (previous.services !== null && sameReferences(previous.services, services)) return previous
  return { ...previous, services }
}

function isResolved(incident: IncidentDTO): incident is IncidentResolvedEventPayload {
  return incident.resolvedAt !== null
}

function noteClosure(
  lifecycle: ReconciliationState['lifecycle'], incident: IncidentDTO,
): ReconciliationState['lifecycle'] {
  const accepted = lifecycle.get(incident.serviceId)
  const closedThrough = !accepted || compareTimestamps(incident.startedAt, accepted.closedThrough) > 0
    ? incident.startedAt : accepted.closedThrough
  const latestResolution = isResolved(incident)
    && (!accepted?.latestResolution || compareTimestamps(incident.startedAt, accepted.latestResolution.startedAt) > 0)
    ? incident : accepted?.latestResolution ?? null
  if (accepted?.closedThrough === closedThrough && accepted.latestResolution === latestResolution) return lifecycle
  const next = new Map(lifecycle)
  next.set(incident.serviceId, { closedThrough, latestResolution })
  return next
}

function knownResolution(state: ReconciliationState, incident: IncidentDTO): IncidentResolvedEventPayload | undefined {
  const visible = state.incidents?.resolved.find(value => incidentKey(value) === incidentKey(incident))
  if (visible && isResolved(visible)) return visible
  const retained = state.lifecycle.get(incident.serviceId)?.latestResolution
  return retained && incidentKey(retained) === incidentKey(incident) ? retained : undefined
}

function selectResolution(
  state: ReconciliationState, incoming: IncidentDTO, issues: ReconciliationIssue[],
): IncidentDTO {
  const accepted = knownResolution(state, incoming)
  if (!accepted) return incoming
  if (incoming.resolvedAt === null) {
    issues.push(incidentIssue('incident-conflict', incoming, 'An accepted resolution cannot become active again.'))
  } else if (compareTimestamps(incoming.resolvedAt, accepted.resolvedAt) !== 0) {
    issues.push(incidentIssue('resolution-conflict', incoming, 'An accepted resolution timestamp cannot change.'))
  }
  return accepted
}

function orderedFeed(active: IncidentDTO[], resolved: IncidentDTO[], previous: IncidentsResponse | null): IncidentsResponse {
  // Stable ties retain REST order or, for inserted events, receipt order. The API
  // does not expose the backend's database-ID tie breaker.
  active.sort((a, b) => compareTimestamps(b.startedAt, a.startedAt))
  resolved.sort((a, b) => compareTimestamps(b.resolvedAt!, a.resolvedAt!))
  const bounded = resolved.slice(0, Math.max(0, incidentFeedTarget - active.length))
  if (previous && sameReferences(previous.active, active) && sameReferences(previous.resolved, bounded)) return previous
  return { active, resolved: bounded }
}

function acceptIncidentSnapshot(
  previous: ReconciliationState, snapshot: IncidentsResponse, issues: ReconciliationIssue[],
): ReconciliationState {
  const startIssues = issues.length
  const activeKeys = new Set(snapshot.active.map(incidentKey))
  let lifecycle = previous.lifecycle
  // Only the caller's pre-request baseline supplies absence evidence. Supplied
  // events are replayed AFTER this step; a new opening is never retired by absence.
  for (const incident of previous.incidents?.active ?? []) {
    if (!activeKeys.has(incidentKey(incident))) lifecycle = noteClosure(lifecycle, incident)
  }

  const keys = new Set<string>()
  const acceptedByKey = new Map([
    ...(previous.incidents?.active ?? []), ...(previous.incidents?.resolved ?? []),
  ].map(incident => [incidentKey(incident), incident]))
  for (const incoming of [...snapshot.active, ...snapshot.resolved]) {
    if (keys.has(incidentKey(incoming))) issues.push(incidentIssue('duplicate-incident', incoming, 'The feed contains a duplicate incident identity.'))
    keys.add(incidentKey(incoming))
  }
  const resolved = snapshot.resolved.map(incoming => {
    const selected = selectResolution(previous, incoming, issues)
    lifecycle = noteClosure(lifecycle, selected)
    return selected
  })
  const activeServices = new Set<string>()
  const active = snapshot.active.map(incoming => {
    selectResolution(previous, incoming, issues)
    const evidence = lifecycle.get(incoming.serviceId)
    if (activeServices.has(incoming.serviceId)) {
      issues.push(incidentIssue('incident-conflict', incoming, 'A service cannot have multiple active incidents.'))
    }
    activeServices.add(incoming.serviceId)
    if (evidence && (compareTimestamps(incoming.startedAt, evidence.closedThrough) <= 0
      || (evidence.latestResolution && compareTimestamps(incoming.startedAt, evidence.latestResolution.resolvedAt) < 0))) {
      issues.push(incidentIssue('incident-conflict', incoming, 'The active incident contradicts accepted closure evidence.'))
    }
    const accepted = acceptedByKey.get(incidentKey(incoming))
    return accepted?.resolvedAt === null ? accepted : incoming
  })
  if (issues.length !== startIssues) return previous
  const incidents = orderedFeed(active, resolved, previous.incidents)
  return incidents === previous.incidents && lifecycle === previous.lifecycle ? previous : { ...previous, incidents, lifecycle }
}

function applyEvent(state: ReconciliationState, event: LiveEvent, issues: ReconciliationIssue[]): ReconciliationState {
  if (event.type === 'status.changed') {
    const { payload } = event
    if (state.services === null) {
      issues.push({ kind: 'missing-baseline', resource: 'status', serviceId: payload.serviceId, message: 'A status snapshot is required before status events.' })
      return state
    }
    const index = state.services.findIndex(service => service.id === payload.serviceId)
    if (index < 0) {
      issues.push({ kind: 'unknown-service', resource: 'status', serviceId: payload.serviceId, message: 'The event service is absent from the REST catalogue.' })
      return state
    }
    const service = state.services[index]
    const status = selectStatus(service.status, event.payload, service.id, issues)
    if (status === service.status) return state
    // SSE payloads contain serviceId; keep it out of the public status DTO.
    const services = [...state.services]
    services[index] = { ...service, status: { state: status.state, lastCheckedAt: status.lastCheckedAt } }
    return { ...state, services }
  }

  const { payload } = event
  if (state.incidents === null) {
    issues.push(incidentIssue('missing-baseline', payload, 'An incident snapshot is required before incident events.'))
    return state
  }
  const key = incidentKey(payload)
  const active = state.incidents.active.find(incident => incidentKey(incident) === key)
  const resolved = knownResolution(state, payload)
  if (resolved) {
    if (event.type === 'incident.resolved') selectResolution(state, event.payload, issues)
    return state
  }
  if (active && event.type === 'incident.opened') return state
  if (!active && !state.services?.some(service => service.id === payload.serviceId)) {
    issues.push(incidentIssue(state.services === null ? 'missing-baseline' : 'unknown-service', payload, 'The event cannot introduce an incident without a known catalogue service.'))
    return state
  }

  const evidence = state.lifecycle.get(payload.serviceId)
  const order = evidence ? compareTimestamps(payload.startedAt, evidence.closedThrough) : 1
  if (event.type === 'incident.opened' ? order <= 0 : order < 0) return state
  if (event.type === 'incident.resolved' && evidence && order === 0 && payload.startedAt !== evidence.closedThrough) {
    issues.push(incidentIssue('incident-conflict', payload, 'Equal start instants have different raw incident identities.'))
    return state
  }
  const otherActive = state.incidents.active.find(incident => incident.serviceId === payload.serviceId && incidentKey(incident) !== key)
  if (otherActive) {
    const startOrder = compareTimestamps(payload.startedAt, otherActive.startedAt)
    if (event.type === 'incident.opened' && startOrder < 0) return state
    if (event.type === 'incident.opened' || startOrder >= 0
      || compareTimestamps(event.payload.resolvedAt, otherActive.startedAt) > 0) {
      issues.push(incidentIssue('incident-conflict', payload, 'The event contradicts another active incident for this service.'))
      return state
    }
  }
  const latest = evidence?.latestResolution
  if (latest && compareTimestamps(payload.startedAt, latest.startedAt) > 0
    && compareTimestamps(payload.startedAt, latest.resolvedAt) < 0) {
    issues.push(incidentIssue('incident-conflict', payload, 'The event overlaps an accepted resolved incident.'))
    return state
  }

  const lifecycle = event.type === 'incident.resolved' ? noteClosure(state.lifecycle, event.payload) : state.lifecycle
  const incidents = event.type === 'incident.opened'
    ? orderedFeed([...state.incidents.active, event.payload], [...state.incidents.resolved], state.incidents)
    : orderedFeed(state.incidents.active.filter(incident => incidentKey(incident) !== key), [...state.incidents.resolved, event.payload], state.incidents)
  return { ...state, incidents, lifecycle }
}

function result(state: ReconciliationState, issues: ReconciliationIssue[]): ReconciliationResult {
  const relevant = new Set([
    ...(state.services?.map(service => service.id) ?? []),
    ...(state.incidents?.active.map(incident => incident.serviceId) ?? []),
    ...(state.incidents?.resolved.map(incident => incident.serviceId) ?? []),
  ])
  const obsolete = [...state.lifecycle.keys()].filter(id => !relevant.has(id))
  if (obsolete.length > 0) {
    const lifecycle = new Map(state.lifecycle)
    obsolete.forEach(id => lifecycle.delete(id))
    state = { ...state, lifecycle }
  }
  return { state, successful: issues.length === 0, issues }
}

// Inputs are validated DTOs. The caller owns request generations and supplies the
// pre-request baseline plus events received since its request barrier, in order.
// The endpoints are independent: a conflicting resource preserves its whole slice.
export function reconcileSnapshots(
  previous: ReconciliationState, snapshots: ReconciliationSnapshots, events: readonly LiveEvent[] = [],
): ReconciliationResult {
  const issues: ReconciliationIssue[] = []
  let state = previous
  if (snapshots.status !== undefined) state = acceptStatusSnapshot(state, snapshots.status, issues)
  if (snapshots.incidents !== undefined) state = acceptIncidentSnapshot(state, snapshots.incidents, issues)
  for (const event of events) state = applyEvent(state, event, issues)
  return result(state, issues)
}

export function applyLiveEvent(previous: ReconciliationState, event: LiveEvent): ReconciliationResult {
  const issues: ReconciliationIssue[] = []
  return result(applyEvent(previous, event, issues), issues)
}
