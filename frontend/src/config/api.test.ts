import { expect, it } from 'vitest'
import { apiPaths } from './api'

it('defines same-origin REST and SSE paths matching the Go router', () => {
  expect(apiPaths).toEqual({
    status: '/api/v1/status',
    incidents: '/api/v1/incidents',
    events: '/api/v1/events',
  })
})
