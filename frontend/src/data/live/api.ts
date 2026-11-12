import { apiPaths } from '@/config/api'
import type { IncidentsResponse, StatusResponse } from '../contracts'
import { validateIncidentsResponse, validateStatusResponse } from './validation'

export type ApiErrorKind = 'http' | 'network' | 'invalid-json' | 'invalid-response'

export class ApiError extends Error {
  readonly kind: ApiErrorKind
  readonly endpoint: string
  readonly status: number | undefined

  constructor(kind: ApiErrorKind, endpoint: string, message: string, options: { status?: number; cause?: unknown } = {}) {
    super(`${endpoint}: ${message}`, { cause: options.cause })
    this.name = 'ApiError'
    this.kind = kind
    this.endpoint = endpoint
    this.status = options.status
  }
}

async function requestSnapshot<T>(endpoint: string, validate: (value: unknown) => asserts value is T, signal?: AbortSignal): Promise<T> {
  signal?.throwIfAborted()
  let response: Response
  try {
    response = await fetch(endpoint, { method: 'GET', headers: { Accept: 'application/json' }, cache: 'no-store', signal })
  } catch (cause) {
    signal?.throwIfAborted()
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw new ApiError('network', endpoint, 'Request failed', { cause })
  }
  signal?.throwIfAborted()
  if (!response.ok) throw new ApiError('http', endpoint, `HTTP ${response.status}`, { status: response.status })

  let body: unknown
  try {
    body = await response.json()
  } catch (cause) {
    signal?.throwIfAborted()
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    const kind = cause instanceof SyntaxError ? 'invalid-json' : 'network'
    throw new ApiError(kind, endpoint, kind === 'invalid-json' ? 'Response is not valid JSON' : 'Response body could not be read', { cause })
  }
  signal?.throwIfAborted()
  try {
    validate(body)
  } catch (cause) {
    throw new ApiError('invalid-response', endpoint, cause instanceof Error ? cause.message : 'Invalid response structure', { cause })
  }
  return body
}

// Independently fetched snapshots have no shared server revision or atomicity guarantee.
export function fetchStatusSnapshot(signal?: AbortSignal): Promise<StatusResponse> {
  return requestSnapshot(apiPaths.status, validateStatusResponse, signal)
}

export function fetchIncidentsSnapshot(signal?: AbortSignal): Promise<IncidentsResponse> {
  return requestSnapshot(apiPaths.incidents, validateIncidentsResponse, signal)
}
