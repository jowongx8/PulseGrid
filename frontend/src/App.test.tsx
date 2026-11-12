import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from '@/App'
import { ThemeProvider } from '@/ThemeProvider'
import { useLiveStatus } from '@/data/live/useLiveStatus'
import type { LiveStatusErrors, LiveStatusSnapshot } from '@/data/live/coordinator'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'
import { mockResizeObserver } from '@/test/resize-observer'
import { mockDialog } from '@/test/dialog'
import { makeService } from '@/test/services'

vi.mock('@/data/live/useLiveStatus', () => ({ useLiveStatus: vi.fn() }))

const checkedAt = '2026-11-10T20:00:00.123456789+08:00'
const alpha = { ...makeService('live-alpha', 'up', checkedAt), name: 'Alpha Grid' }
const beta = { ...makeService('live-beta', 'down', checkedAt), name: 'Beta Grid' }
const incidents = {
  active: [{ serviceId: beta.id, startedAt: '2026-11-10T12:00:01.123456Z', resolvedAt: null }],
  resolved: [{ serviceId: 'legacy', startedAt: '2026-11-10T10:00:00.123456Z', resolvedAt: '2026-11-10T11:00:00.123457Z' }],
}

function snapshot(overrides: Partial<LiveStatusSnapshot> = {}, errors: Partial<LiveStatusErrors> = {}): LiveStatusSnapshot {
  return {
    services: null, incidents: null, transport: 'connecting', synchronization: 'waiting', ...overrides,
    errors: { status: null, incidents: null, stream: null, reconciliation: [], recovery: null, ...errors },
  }
}

let observer: ReturnType<typeof mockResizeObserver>
let dialog: ReturnType<typeof mockDialog>
beforeEach(() => {
  mockThemeEnvironment()
  observer = mockResizeObserver()
  dialog = mockDialog()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot())
})
afterEach(() => { dialog.restore(); resetThemeTestState() })

const app = () => <ThemeProvider><App /></ThemeProvider>
function measure() {
  act(() => observer.resize(screen.getByRole('group', { name: 'Service treemap' }), 1000, 600))
}
function connection() {
  return within(screen.getByRole('status', { name: 'Dashboard connection' }))
}

it('preserves the initial shell and unknown counts instead of rendering fixtures or an empty catalogue', () => {
  render(app())
  expect(screen.getByRole('heading', { name: 'PulseGrid', level: 1 })).toBeInTheDocument()
  expect(screen.getByRole('main', { name: 'Service map' })).toHaveAttribute('aria-busy', 'true')
  expect(screen.getByRole('status', { name: 'Loading service statuses' })).toBeInTheDocument()
  expect(connection().getByText('Connecting')).toBeInTheDocument()
  expect(screen.getByLabelText('Down count unavailable')).toHaveTextContent('—')
  expect(screen.queryByText(/0 services monitored|No services available|GitHub|Demo/)).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Search services' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Incidents' })).toBeEnabled()
})

it('passes accepted live DTOs into counts, the treemap and the incident drawer', async () => {
  const fetch = vi.fn()
  vi.stubGlobal('fetch', fetch)
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha, beta], incidents, transport: 'open', synchronization: 'synchronized' }))
  render(app())
  measure()
  expect(connection().getByText('Live')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '1 service down' })).toBeInTheDocument()
  expect(screen.getByText('50% healthy · 2 services monitored')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Alpha Grid: Operational' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Beta Grid: Down' })).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Incidents (1 active)' }))
  expect(screen.getByText('Unavailable service (legacy)')).toBeInTheDocument()
  expect(screen.queryByText('Showing last-known incident information.')).not.toBeInTheDocument()
  expect(screen.queryByText(/Last checked|Last updated|Latest service check/)).not.toBeInTheDocument()
  expect(fetch).not.toHaveBeenCalled()
})

it.each([0, 2])('keeps both resources loading after only an SSE error (readyState %s)', async readyState => {
  const { rerender } = render(app())
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ transport: readyState === 2 ? 'closed' : 'reconnecting', synchronization: 'unsynchronized' }, { stream: { type: 'transport-error', readyState } }))
  rerender(app())
  expect(screen.getByRole('status', { name: 'Loading service statuses' })).toBeInTheDocument()
  expect(screen.queryByText('Unable to load service statuses')).not.toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Incidents' }))
  expect(screen.getByRole('status', { name: 'Loading incidents' })).toBeInTheDocument()
  expect(screen.queryByText('Incidents unavailable')).not.toBeInTheDocument()
})

it('latches only actual Live and preserves that presentation history through subsequent failures', () => {
  const { rerender } = render(app())
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha], transport: 'open', synchronization: 'synchronizing' }))
  rerender(app())
  expect(connection().getByText('Connecting')).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha], transport: 'reconnecting', synchronization: 'unsynchronized' }))
  rerender(app())
  expect(connection().getByText('Offline')).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha], incidents, transport: 'open', synchronization: 'synchronized' }))
  rerender(app())
  expect(connection().getByText('Live')).toBeInTheDocument()
  for (const transport of ['reconnecting', 'closed', 'connecting', 'open'] as const) {
    vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha], incidents, transport, synchronization: 'synchronizing' }))
    rerender(app())
    expect(connection().getByText(transport === 'closed' ? 'Offline' : 'Reconnecting')).toBeInTheDocument()
  }
})

it('keeps a failed initial resource unavailable during retries until accepted data arrives', async () => {
  const error = new Error('Technical detail must not be displayed')
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ transport: 'open', synchronization: 'unsynchronized' }, { status: error }))
  const { rerender } = render(app())
  expect(connection().getByText('Reconnecting')).toBeInTheDocument()
  expect(screen.getByText('Unable to load service statuses')).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ transport: 'open', synchronization: 'synchronizing' }, { status: error }))
  rerender(app())
  expect(screen.queryByRole('status', { name: 'Loading service statuses' })).not.toBeInTheDocument()
  expect(screen.getByText('Unable to load service statuses')).toBeInTheDocument()
  expect(screen.queryByText(error.message)).not.toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [], incidents: { active: [], resolved: [] }, transport: 'open', synchronization: 'synchronized' }))
  rerender(app())
  expect(screen.getByRole('heading', { name: 'No services available' })).toBeInTheDocument()
  expect(screen.getByText('0 services monitored')).toBeInTheDocument()
  expect(connection().getByText('Live')).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Incidents' }))
  expect(screen.getByText('No incidents to display')).toBeInTheDocument()
})

it('keeps accepted services interactive when incidents fail and retains the incident error during retries', async () => {
  const error = new Error('Incident request failed')
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha, beta], transport: 'open', synchronization: 'unsynchronized' }, { incidents: error }))
  const { rerender } = render(app())
  measure()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Beta Grid: Down' }))
  expect(screen.getByRole('dialog', { name: 'Beta Grid' })).toBeInTheDocument()
  expect(screen.getByText('Incident information unavailable')).toBeInTheDocument()
  expect(screen.queryByText('No active incidents.')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Close service details' }))
  await user.click(screen.getByRole('button', { name: 'Incidents' }))
  expect(screen.getByText('Incidents unavailable')).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha, beta], transport: 'open', synchronization: 'synchronizing' }, { incidents: error }))
  rerender(app())
  expect(screen.getByText('Incidents unavailable')).toBeInTheDocument()
  expect(screen.queryByRole('status', { name: 'Loading incidents' })).not.toBeInTheDocument()
})

it('accepts incident-only data and contextualizes only the failed status resource', async () => {
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ incidents, transport: 'open', synchronization: 'unsynchronized' }, {
    reconciliation: [{ kind: 'duplicate-service', resource: 'status', message: 'Internal conflict' }],
  }))
  render(app())
  expect(screen.getByText('Unable to load service statuses')).toBeInTheDocument()
  expect(screen.getByLabelText('Down count unavailable')).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Incidents (1 active)' }))
  expect(screen.getByText('Unavailable service (live-beta)')).toBeInTheDocument()
  expect(screen.queryByText('Incidents unavailable')).not.toBeInTheDocument()
})

it('retains initial resource failures when later diagnostics concern another resource', async () => {
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ transport: 'open', synchronization: 'unsynchronized' }, {
    reconciliation: [{ kind: 'duplicate-service', resource: 'status', message: 'Invalid catalogue' }],
  }))
  const { rerender } = render(app())
  expect(screen.getByText('Unable to load service statuses')).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ transport: 'open', synchronization: 'synchronizing' }, {
    reconciliation: [{ kind: 'missing-baseline', resource: 'incidents', message: 'Missing baseline' }],
  }))
  rerender(app())
  expect(screen.getByText('Unable to load service statuses')).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Incidents' }))
  expect(screen.getByText('Incidents unavailable')).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ transport: 'open', synchronization: 'synchronizing' }))
  rerender(app())
  expect(screen.getByText('Unable to load service statuses')).toBeInTheDocument()
  expect(screen.getByText('Incidents unavailable')).toBeInTheDocument()
  expect(connection().getByText('Reconnecting')).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha], incidents: { active: [], resolved: [] }, transport: 'open', synchronization: 'synchronized' }))
  rerender(app())
  expect(screen.queryByText('Unable to load service statuses')).not.toBeInTheDocument()
  expect(screen.queryByText('Incidents unavailable')).not.toBeInTheDocument()
  expect(screen.getByText('No incidents to display')).toBeInTheDocument()
})

it('retains services, selection, incidents and geometry while synchronization becomes unhealthy', async () => {
  const accepted = snapshot({ services: [alpha, beta], incidents, transport: 'open', synchronization: 'synchronized' })
  vi.mocked(useLiveStatus).mockReturnValue(accepted)
  const { rerender } = render(app())
  measure()
  const user = userEvent.setup()
  const tile = screen.getByRole('button', { name: 'Alpha Grid: Operational' })
  const placement = tile.closest('li')?.getAttribute('style')
  await user.click(tile)
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ ...accepted, transport: 'reconnecting', synchronization: 'unsynchronized' }, { status: new Error('Unavailable'), incidents: new Error('Unavailable') }))
  rerender(app())
  expect(screen.getByRole('dialog', { name: 'Alpha Grid' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Alpha Grid: Operational' })).toBe(tile)
  expect(tile.closest('li')?.getAttribute('style')).toBe(placement)
  await user.click(screen.getByRole('button', { name: 'Close service details' }))
  await user.click(screen.getByRole('button', { name: 'Incidents (1 active)' }))
  expect(screen.getByText('Showing last-known incident information.')).toBeInTheDocument()
  expect(screen.getByText('Unavailable service (legacy)')).toBeInTheDocument()
  expect(screen.queryByText('Unable to load service statuses')).not.toBeInTheDocument()
  expect(screen.queryByText('Incidents unavailable')).not.toBeInTheDocument()
})

it('updates search membership and closes selected removed services without retaining stale DTOs', async () => {
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [alpha, beta], incidents, transport: 'open', synchronization: 'synchronized' }))
  const { rerender } = render(app())
  measure()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Search services' }))
  await user.type(screen.getByRole('combobox'), 'alpha')
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [beta], incidents, transport: 'open', synchronization: 'synchronized' }))
  rerender(app())
  expect(screen.getByText('No services match your search.')).toBeInTheDocument()
  await user.clear(screen.getByRole('combobox'))
  await user.type(screen.getByRole('combobox'), 'beta')
  await user.keyboard('{Enter}')
  expect(screen.getByRole('dialog', { name: 'Beta Grid' })).toBeInTheDocument()
  vi.mocked(useLiveStatus).mockReturnValue(snapshot({ services: [], incidents, transport: 'open', synchronization: 'synchronized' }))
  rerender(app())
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(screen.getByRole('button', { name: 'Search services' })).toHaveFocus()
})
