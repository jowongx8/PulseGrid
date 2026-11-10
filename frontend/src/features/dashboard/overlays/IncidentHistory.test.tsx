import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import type { IncidentsResponse } from '@/data/contracts'
import { makeService } from '@/test/services'
import { IncidentHistory } from './IncidentHistory'

it('orders exact instants within active/resolved sections and preserves supplied ties and raw identity', () => {
  const a = makeService('a', 'unknown')
  const b = makeService('b', 'up')
  const feed: IncidentsResponse = {
    active: [
      { serviceId: 'a', startedAt: '2026-11-10T12:00:00Z', resolvedAt: null },
      { serviceId: 'b', startedAt: '2026-11-10T20:00:00.000001+08:00', resolvedAt: null },
    ],
    resolved: [
      { serviceId: 'a', startedAt: '2026-11-10T11:00:00.123400Z', resolvedAt: '2026-11-10T12:00:00.12Z' },
      { serviceId: 'b', startedAt: '2026-11-10T11:01:00Z', resolvedAt: '2026-11-10T07:00:00.120001-05:00' },
      { serviceId: 'a', startedAt: '2026-11-10T11:02:00Z', resolvedAt: '2026-11-10T12:00:00.120001Z' },
    ],
  }
  const before = structuredClone(feed)
  render(<IncidentHistory services={[a, b]} incidents={feed} />)
  const sections = screen.getAllByRole('region')
  expect(sections[0]).toHaveAccessibleName('Active incidents')
  const active = within(sections[0]).getAllByRole('listitem')
  expect(active.map((row) => row.dataset.incidentKey)).toEqual(['b|2026-11-10T20:00:00.000001+08:00', 'a|2026-11-10T12:00:00Z'])
  const resolved = within(sections[1]).getAllByRole('listitem')
  expect(resolved.map((row) => row.dataset.incidentKey)).toEqual(['b|2026-11-10T11:01:00Z', 'a|2026-11-10T11:02:00Z', 'a|2026-11-10T11:00:00.123400Z'])
  expect(resolved[2].querySelector('time')).toHaveAttribute('datetime', '2026-11-10T11:00:00.123400Z')
  expect(feed).toEqual(before)
})

it('opens available services and keeps missing references visible without an action', async () => {
  const user = userEvent.setup()
  const onSelectService = vi.fn()
  render(<IncidentHistory services={[makeService('a', 'unknown')]} onSelectService={onSelectService} incidents={{
    active: [{ serviceId: 'a', startedAt: '2026-11-10T12:00:00Z', resolvedAt: null }, { serviceId: 'missing', startedAt: '2026-11-10T11:00:00Z', resolvedAt: null }],
    resolved: [{ serviceId: 'a', startedAt: '2026-11-10T10:00:00Z', resolvedAt: null }],
  }} />)
  const active = within(screen.getByRole('region', { name: 'Active incidents' }))
  await user.click(active.getByRole('button', { name: /Service a/ }))
  expect(onSelectService).toHaveBeenCalledWith('a')
  expect(active.getByText('Unavailable service (missing)')).toBeInTheDocument()
  expect(active.getAllByRole('button')).toHaveLength(1)
  expect(screen.getByText('Service details unavailable.')).toBeInTheDocument()
  expect(screen.getByText('Resolution time unavailable')).toBeInTheDocument()
})

it('shows both feed empty states', () => {
  render(<IncidentHistory services={[]} incidents={{ active: [], resolved: [] }} />)
  expect(screen.getByText('No active incidents.')).toBeInTheDocument()
  expect(screen.getByText('No recently resolved incidents.')).toBeInTheDocument()
})
