// Public JSON contracts from backend/internal/httpapi/{status,incidents,events}.go.

// Preserve wire strings, including precision and offsets; do not reserialize them.
export type ApiTimestamp = string

export type ServiceState = 'up' | 'down' | 'unknown'

export type ServiceCategory =
  | 'developer-cloud'
  | 'ai'
  | 'social-media'
  | 'communication'
  | 'productivity'
  | 'entertainment'
  | 'gaming'
  | 'commerce'

export interface CurrentStatusDTO {
  state: ServiceState
  lastCheckedAt: ApiTimestamp | null
}

export interface ServiceDTO {
  id: string
  name: string
  category: ServiceCategory
  websiteUrl: string
  weight: number
  status: CurrentStatusDTO
}

export interface StatusResponse {
  services: ServiceDTO[]
}

// Incident identity is serviceId + "|" + the unchanged startedAt string.
export interface IncidentDTO {
  serviceId: string
  startedAt: ApiTimestamp
  resolvedAt: ApiTimestamp | null
}

export interface IncidentsResponse {
  active: IncidentDTO[]
  resolved: IncidentDTO[]
}

// event: status.changed
export interface StatusChangedEventPayload {
  serviceId: string
  state: ServiceState
  lastCheckedAt: ApiTimestamp
}

// event: incident.opened
export interface IncidentOpenedEventPayload extends IncidentDTO {
  resolvedAt: null
}

// event: incident.resolved
export interface IncidentResolvedEventPayload extends IncidentDTO {
  resolvedAt: ApiTimestamp
}
