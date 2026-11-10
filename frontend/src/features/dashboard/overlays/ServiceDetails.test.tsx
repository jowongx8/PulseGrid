import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { makeService } from '@/test/services'
import { ServiceDetails } from './ServiceDetails'

it.each(['up', 'down', 'unknown'] as const)('shows available metadata and incidents for an %s service', (state) => {
  const service = { ...makeService('a', state, '2026-11-10T20:00:00.123456789+08:00'), websiteUrl: 'https://example.com/official' }
  const { container } = render(<ServiceDetails service={service} incidents={{
    active: [{ serviceId: 'a', startedAt: '2026-11-10T11:00:00.123456Z', resolvedAt: null }],
    resolved: [{ serviceId: 'other', startedAt: '2026-11-10T09:00:00Z', resolvedAt: '2026-11-10T10:00:00Z' }],
  }} />)
  expect(screen.getByText(state === 'up' ? 'Operational' : state === 'down' ? 'Down' : 'Unknown')).toBeInTheDocument()
  expect(container.querySelector('time')).toHaveAttribute('datetime', service.status.lastCheckedAt)
  const website = screen.getByRole('link', { name: 'https://example.com/official' })
  expect(website).toHaveAttribute('rel', 'noopener noreferrer')
  expect(screen.getByText('Ongoing')).toBeInTheDocument()
  expect(screen.getByText('No recently resolved incidents.')).toBeInTheDocument()
  expect(screen.queryByText(/other/)).not.toBeInTheDocument()
})

it('handles missing checks, website and selected service', () => {
  const { rerender } = render(<ServiceDetails service={{ ...makeService('a', 'unknown'), websiteUrl: '' }} incidents={{ active: [], resolved: [] }} />)
  expect(screen.getByText('No service checks recorded')).toBeInTheDocument()
  expect(screen.getByText('Website unavailable')).toBeInTheDocument()
  rerender(<ServiceDetails service={undefined} incidents={{ active: [], resolved: [] }} />)
  expect(screen.getByRole('status')).toHaveTextContent('This service is no longer available.')
})
