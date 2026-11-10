import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { ServiceState } from '@/data/contracts'
import { makeService } from '@/test/services'
import { StatusSummary } from './StatusSummary'

describe('StatusSummary', () => {
  it('renders derived totals and informational counts in the specified order', () => {
    render(<StatusSummary services={[
      makeService('a', 'up'), makeService('b', 'up'),
      makeService('c', 'down'), makeService('d', 'unknown'),
    ]} />)

    expect(screen.getByRole('heading', { name: '1 service down' })).toBeInTheDocument()
    expect(screen.getByText('50% healthy · 4 services monitored')).toBeInTheDocument()
    const chips = within(screen.getByRole('list', { name: 'Service counts' })).getAllByRole('listitem')
    expect(chips.map((chip) => chip.textContent)).toEqual(['1Down', '2Operational', '1Unknown'])
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it.each<[ServiceState[], string]>([
    [['up'], 'All monitored services operational'],
    [['unknown'], '1 service status unknown'],
    [['up', 'unknown', 'unknown'], '2 service statuses unknown'],
    [['unknown', 'down', 'down'], '2 services down'],
  ])('uses the appropriate headline for %j', (states, headline) => {
    render(<StatusSummary services={states.map((state, i) => makeService(`${i}`, state))} />)
    expect(screen.getByRole('heading', { name: headline })).toBeInTheDocument()
  })

  it('renders an empty collection without an invented health percentage or check time', () => {
    render(<StatusSummary services={[]} />)
    expect(screen.getByRole('heading', { name: 'No services monitored' })).toBeInTheDocument()
    expect(screen.getByText('0 services monitored')).toBeInTheDocument()
    expect(screen.getByText('No service checks recorded')).toBeInTheDocument()
    expect(screen.queryByText(/% healthy/)).not.toBeInTheDocument()
    expect(screen.getAllByRole('listitem').every((chip) => chip.textContent?.startsWith('0'))).toBe(true)
  })

  it('handles all-null checks for a non-empty collection', () => {
    const { container } = render(<StatusSummary services={[makeService('unobserved', 'unknown')]} />)
    expect(screen.getByText('No service checks recorded')).toBeInTheDocument()
    expect(container.querySelector('time')).toBeNull()
  })

  it('displays the newest observed Unknown check and retains its exact API string', () => {
    const latest = '2026-09-24T20:00:00.123456789+08:00'
    const { container } = render(<StatusSummary services={[
      makeService('older', 'up', '2026-09-24T12:00:00.123456788Z'),
      makeService('unobserved', 'unknown'),
      makeService('observed', 'unknown', latest),
    ]} />)

    expect(screen.getByText(/Latest service check:/)).toBeInTheDocument()
    expect(container.querySelector('time')).toHaveAttribute('datetime', latest)
    expect(container.querySelector('time')).toHaveTextContent(/GMT/)
    expect(screen.queryByText(/sync|just now|live/i)).not.toBeInTheDocument()
  })
})
