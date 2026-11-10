import type { IncidentsResponse, ServiceCategory, ServiceState, StatusResponse } from '../contracts'
import { compareTimestamps } from '../timestamps'

// These lookups validate the closed wire contract, independently of UI labels/order.
const categories = {
  'developer-cloud': true,
  ai: true,
  'social-media': true,
  communication: true,
  productivity: true,
  entertainment: true,
  gaming: true,
  commerce: true,
} satisfies Record<ServiceCategory, true>

const states = { up: true, down: true, unknown: true } satisfies Record<ServiceState, true>

function invalid(path: string, expected: string): never {
  throw new TypeError(`${path}: expected ${expected}`)
}

function requireObject(value: unknown, path: string): asserts value is Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) invalid(path, 'an object')
}

function requireArray(value: unknown, path: string): asserts value is unknown[] {
  if (!Array.isArray(value)) invalid(path, 'an array')
}

function requireString(value: unknown, path: string): asserts value is string {
  if (typeof value !== 'string' || value.trim().length === 0) invalid(path, 'a nonempty string')
}

function requireTimestamp(value: unknown, path: string): asserts value is string {
  requireString(value, path)
  // Go JSON emits RFC3339 with optional fractional seconds (up to nanoseconds).
  const match = /^(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.exec(value)
  if (!match || match[0] !== value) invalid(path, 'a Go-compatible RFC3339 timestamp')
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const days = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  if (day > days[month - 1]) invalid(path, 'a valid calendar date')
}

export function validateStatusResponse(value: unknown): asserts value is StatusResponse {
  requireObject(value, 'response')
  requireArray(value.services, 'services')
  value.services.forEach((service, index) => {
    const path = `services[${index}]`
    requireObject(service, path)
    requireString(service.id, `${path}.id`)
    requireString(service.name, `${path}.name`)
    requireString(service.category, `${path}.category`)
    if (!Object.hasOwn(categories, service.category)) invalid(`${path}.category`, 'a supported service category')
    requireString(service.websiteUrl, `${path}.websiteUrl`)
    if (typeof service.weight !== 'number' || !Number.isSafeInteger(service.weight) || service.weight <= 0) invalid(`${path}.weight`, 'a positive safe integer')
    requireObject(service.status, `${path}.status`)
    requireString(service.status.state, `${path}.status.state`)
    if (!Object.hasOwn(states, service.status.state)) invalid(`${path}.status.state`, 'up, down or unknown')
    if (service.status.lastCheckedAt !== null) requireTimestamp(service.status.lastCheckedAt, `${path}.status.lastCheckedAt`)
  })
}

export function validateIncidentsResponse(value: unknown): asserts value is IncidentsResponse {
  requireObject(value, 'response')
  for (const group of ['active', 'resolved'] as const) {
    requireArray(value[group], group)
    value[group].forEach((incident, index) => {
      const path = `${group}[${index}]`
      requireObject(incident, path)
      requireString(incident.serviceId, `${path}.serviceId`)
      requireTimestamp(incident.startedAt, `${path}.startedAt`)
      if (group === 'active') {
        if (incident.resolvedAt !== null) invalid(`${path}.resolvedAt`, 'null for an active incident')
      } else {
        requireTimestamp(incident.resolvedAt, `${path}.resolvedAt`)
        if (compareTimestamps(incident.resolvedAt, incident.startedAt) <= 0) invalid(`${path}.resolvedAt`, 'a timestamp after startedAt')
      }
    })
  }
}
