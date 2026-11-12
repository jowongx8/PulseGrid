import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { makeService } from '@/test/services'
import { IncidentsDrawer } from './IncidentsDrawer'

it('shows noninteractive loading entries while the initial incident resource is pending', () => {
  render(<IncidentsDrawer services={null} incidents={null} onSelectService={vi.fn()} />)
  expect(screen.getByRole('status', { name: 'Loading incidents' })).toHaveAttribute('aria-busy', 'true')
  expect(screen.queryByText('No incidents to display')).not.toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it('shows contextual unavailable copy without an empty feed or Retry button', () => {
  render(<IncidentsDrawer services={null} incidents={null} unavailable onSelectService={vi.fn()} />)
  expect(screen.getByText('Incidents unavailable')).toBeInTheDocument()
  expect(screen.getByText("Unable to load incident information. We'll retry automatically.")).toBeInTheDocument()
  expect(screen.queryByText('No incidents to display')).not.toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it.each([false, true])('distinguishes a valid empty response and retained information (stale: %s)', stale => {
  render(<IncidentsDrawer services={[]} incidents={{ active: [], resolved: [] }} unavailable stale={stale} onSelectService={vi.fn()} />)
  expect(screen.getByText('No incidents to display')).toBeInTheDocument()
  expect(screen.queryByText('Incidents unavailable')).not.toBeInTheDocument()
  expect(screen.queryByText('Showing last-known incident information.') !== null).toBe(stale)
})

it('retains ordered accepted incidents with one note, precise timestamps and legacy references', async () => {
  const onSelect = vi.fn()
  const started = '2026-11-10T12:00:00.123456Z'
  const resolved = '2026-11-10T11:00:00.123457Z'
  const { container } = render(<IncidentsDrawer services={[makeService('a', 'unknown')]} unavailable stale onSelectService={onSelect} incidents={{
    active: [{ serviceId: 'a', startedAt: started, resolvedAt: null }],
    resolved: [{ serviceId: 'legacy', startedAt: '2026-11-10T10:00:00.123456Z', resolvedAt: resolved }],
  }} />)
  expect(screen.getAllByText('Showing last-known incident information.')).toHaveLength(1)
  expect(screen.queryByText('Incidents unavailable')).not.toBeInTheDocument()
  expect(screen.getByText('Unavailable service (legacy)')).toBeInTheDocument()
  expect([...container.querySelectorAll('time')].map(time => time.dateTime)).toEqual([started, '2026-11-10T10:00:00.123456Z', resolved])
  await userEvent.setup().click(within(screen.getByRole('region', { name: 'Active incidents' })).getByRole('button'))
  expect(onSelect).toHaveBeenCalledWith('a')
})
