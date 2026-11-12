import { StrictMode } from 'react'
import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { ServiceState } from '@/data/contracts'
import type { ConnectionStatus } from './live-presentation'
import { makeService } from '@/test/services'
import { StatusSummary } from './StatusSummary'

describe('StatusSummary', () => {
  it('renders derived totals and informational counts in the specified order', () => {
    render(<StatusSummary connectionStatus="live" services={[
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
    render(<StatusSummary connectionStatus="live" services={states.map((state, i) => makeService(`${i}`, state))} />)
    expect(screen.getByRole('heading', { name: headline })).toBeInTheDocument()
  })

  it('renders an empty collection without an invented health percentage or check time', () => {
    render(<StatusSummary connectionStatus="live" services={[]} />)
    expect(screen.getByRole('heading', { name: 'No services monitored' })).toBeInTheDocument()
    expect(screen.getByText('0 services monitored')).toBeInTheDocument()
    expect(screen.queryByText('No service checks recorded')).not.toBeInTheDocument()
    expect(screen.queryByText(/% healthy/)).not.toBeInTheDocument()
    expect(screen.getAllByRole('listitem').every((chip) => chip.textContent?.startsWith('0'))).toBe(true)
  })

  it('does not display check timestamps or modify their raw API values', () => {
    const latest = '2026-09-24T20:00:00.123456789+08:00'
    const services = [
      makeService('older', 'up', '2026-09-24T12:00:00.123456788Z'),
      makeService('unobserved', 'unknown'),
      makeService('observed', 'unknown', latest),
    ]
    const { container } = render(<StatusSummary connectionStatus="live" services={services} />)

    expect(screen.queryByText(/Latest service check|Last updated|No service checks/)).not.toBeInTheDocument()
    expect(container.querySelector('time')).toBeNull()
    expect(services[2].status.lastCheckedAt).toBe(latest)
  })

  it.each([false, true])('shows unknown count placeholders when services are missing (unavailable: %s)', unavailable => {
    render(<StatusSummary connectionStatus="connecting" services={null} unavailable={unavailable} />)
    const counts = within(screen.getByRole('list', { name: 'Service counts' }))
    expect(counts.getAllByRole('listitem').map(chip => chip.textContent)).toEqual(['—Down', '—Operational', '—Unknown'])
    expect(counts.getByLabelText('Down count unavailable')).toBeInTheDocument()
    expect(screen.queryByText(/0 services monitored|% healthy/)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: unavailable ? 'Service statuses unavailable' : 'Loading service statuses…' })).toBeInTheDocument()
  })

  it.each<ConnectionStatus>(['connecting', 'live', 'reconnecting', 'offline'])('renders an accessible %s indicator without announcing counts', connectionStatus => {
    render(<StatusSummary services={null} connectionStatus={connectionStatus} />)
    const indicator = screen.getByRole('status', { name: 'Dashboard connection' })
    expect(indicator).toHaveAttribute('aria-live', 'polite')
    expect(indicator).toHaveAttribute('aria-atomic', 'true')
    expect(within(indicator).getByText(connectionStatus[0].toUpperCase() + connectionStatus.slice(1))).toBeInTheDocument()
    expect(indicator).toHaveClass(connectionStatus === 'live' ? 'text-live' : 'text-muted-foreground')
    expect(indicator).not.toContainElement(screen.getByRole('list', { name: 'Service counts' }))
    expect(indicator.querySelector('svg')).toHaveAttribute('aria-hidden', 'true')
  })

  it.each<[ServiceState[]]>([[['up']], [['down']], [['unknown']], [[]]])('does not announce the initial health of %j under Strict Mode', states => {
    render(<StrictMode><StatusSummary connectionStatus="live" services={states.map((state, i) => makeService(`${i}`, state))} /></StrictMode>)
    expect(screen.getByRole('status', { name: 'Service health updates' })).toBeEmptyDOMElement()
  })

  it('updates the persistent polite region for meaningful health transitions and recovery', () => {
    const { rerender } = render(<StrictMode><StatusSummary connectionStatus="live" services={[makeService('a', 'up')]} /></StrictMode>)
    const announcement = screen.getByRole('status', { name: 'Service health updates' })
    expect(announcement).toHaveClass('sr-only')
    expect(announcement).toHaveAttribute('aria-live', 'polite')
    expect(announcement).toHaveAttribute('aria-atomic', 'true')

    const transitions: [ServiceState[], string][] = [
      [['down'], 'Some monitored services are down'],
      [['unknown'], 'Some monitored service statuses are unknown'],
      [['up'], 'All monitored services operational'],
      [[], 'No services monitored'],
    ]
    for (const [states, message] of transitions) {
      rerender(<StrictMode><StatusSummary connectionStatus="live" services={states.map((state, i) => makeService(`${i}`, state))} /></StrictMode>)
      expect(screen.getByRole('status', { name: 'Service health updates' })).toBe(announcement)
      expect(announcement).toHaveTextContent(message)
    }
  })

  it('does not rewrite the health announcement for counts, successful checks or unrelated rerenders', () => {
    const { rerender } = render(<StatusSummary connectionStatus="live" services={[makeService('a', 'up')]} />)
    const announcement = screen.getByRole('status', { name: 'Service health updates' })

    for (const state of ['down', 'unknown', 'up'] as const) {
      rerender(<StatusSummary connectionStatus="live" services={[makeService('a', state)]} />)
      const announcementText = announcement.firstChild
      expect(announcementText).not.toBeNull()

      const services = [makeService('a', state, '2026-11-10T12:00:00.123456789Z'), makeService('b', state)]
      rerender(<StatusSummary connectionStatus="live" services={services} />)
      rerender(<StatusSummary connectionStatus="live" services={services} unavailable />)
      expect(announcement.firstChild).toBe(announcementText)
    }
  })

  it('keeps initial loading, unavailable and first loaded data silent', () => {
    const { rerender } = render(<StatusSummary connectionStatus="connecting" services={null} />)
    const announcement = screen.getByRole('status', { name: 'Service health updates' })
    expect(announcement).toBeEmptyDOMElement()

    rerender(<StatusSummary connectionStatus="offline" services={null} unavailable />)
    expect(announcement).toBeEmptyDOMElement()
    rerender(<StatusSummary connectionStatus="live" services={[makeService('a', 'down')]} />)
    expect(announcement).toBeEmptyDOMElement()
    rerender(<StatusSummary connectionStatus="live" services={[makeService('a', 'up')]} />)
    expect(announcement).toHaveTextContent('All monitored services operational')
  })

  it('keeps connection announcements separate from health announcements', () => {
    const services = [makeService('a', 'up')]
    const { rerender } = render(<StatusSummary connectionStatus="connecting" services={services} />)
    const connection = screen.getByRole('status', { name: 'Dashboard connection' })
    const health = screen.getByRole('status', { name: 'Service health updates' })
    expect(connection).not.toContainElement(health)

    for (const status of ['live', 'reconnecting', 'offline'] as const) {
      rerender(<StatusSummary connectionStatus={status} services={services} />)
      expect(screen.getByRole('status', { name: 'Dashboard connection' })).toBe(connection)
      expect(within(connection).getByText(status[0].toUpperCase() + status.slice(1))).toBeInTheDocument()
      expect(health).toBeEmptyDOMElement()
    }
  })
})
