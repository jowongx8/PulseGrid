import type { ApiTimestamp, IncidentsResponse, StatusResponse } from '@/data/contracts'

export const fixtureReferenceTimestamp = '2026-09-24T12:00:00.123456789Z' satisfies ApiTimestamp

// Development fixtures are a representative subset of the backend service catalogue.
// The backend remains the production catalogue authority.
export const statusResponseFixture = {
  services: [
    {
      id: 'github',
      name: 'GitHub',
      category: 'developer-cloud',
      websiteUrl: 'https://github.com',
      weight: 10,
      status: { state: 'down', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'cloudflare',
      name: 'Cloudflare',
      category: 'developer-cloud',
      websiteUrl: 'https://www.cloudflare.com',
      weight: 8,
      status: { state: 'up', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'openai',
      name: 'OpenAI',
      category: 'ai',
      websiteUrl: 'https://openai.com',
      weight: 9,
      status: { state: 'up', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'instagram',
      name: 'Instagram',
      category: 'social-media',
      websiteUrl: 'https://www.instagram.com',
      weight: 8,
      status: { state: 'up', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'discord',
      name: 'Discord',
      category: 'communication',
      websiteUrl: 'https://discord.com',
      weight: 8,
      status: { state: 'unknown', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'slack',
      name: 'Slack',
      category: 'communication',
      websiteUrl: 'https://slack.com',
      weight: 7,
      status: { state: 'unknown', lastCheckedAt: null },
    },
    {
      id: 'google-drive',
      name: 'Google Drive',
      category: 'productivity',
      websiteUrl: 'https://drive.google.com',
      weight: 9,
      status: { state: 'up', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'netflix',
      name: 'Netflix',
      category: 'entertainment',
      websiteUrl: 'https://www.netflix.com',
      weight: 8,
      status: { state: 'up', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'playstation-network',
      name: 'PlayStation Network',
      category: 'gaming',
      websiteUrl: 'https://www.playstation.com',
      weight: 7,
      status: { state: 'up', lastCheckedAt: fixtureReferenceTimestamp },
    },
    {
      id: 'amazon',
      name: 'Amazon',
      category: 'commerce',
      websiteUrl: 'https://www.amazon.com',
      weight: 9,
      status: { state: 'up', lastCheckedAt: fixtureReferenceTimestamp },
    },
  ],
} satisfies StatusResponse

// Each feed group follows the backend's newest-first ordering convention.
export const incidentsResponseFixture = {
  active: [
    {
      serviceId: 'github',
      startedAt: '2026-09-24T11:42:00.123456Z',
      resolvedAt: null,
    },
  ],
  resolved: [
    {
      serviceId: 'openai',
      startedAt: '2026-09-24T11:20:00.234567Z',
      resolvedAt: '2026-09-24T11:50:00.654321Z',
    },
  ],
} satisfies IncidentsResponse
