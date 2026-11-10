// Browser requests stay same-origin in development and behind CloudFront.
export const apiPaths = {
  status: '/api/v1/status',
  incidents: '/api/v1/incidents',
  events: '/api/v1/events',
} as const
