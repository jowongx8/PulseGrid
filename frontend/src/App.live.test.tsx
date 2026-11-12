import { StrictMode } from 'react'
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from '@/App'
import { ThemeProvider } from '@/ThemeProvider'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'
import { mockResizeObserver } from '@/test/resize-observer'
import { mockDialog } from '@/test/dialog'
import { makeService } from '@/test/services'
import * as layout from '@/features/dashboard/treemap/layout'

const t1 = '2026-11-12T00:00:01.123456789Z'
const t2 = '2026-11-12T00:00:02.123456790Z'
const services = [{ ...makeService('actual-owner', 'up', t1), name: 'Live Service' }, { ...makeService('observed-unknown', 'unknown', t1), name: 'Unknown Service' }]
const feed = { active: [{ serviceId: 'observed-unknown', startedAt: '2026-11-12T00:00:00.123456Z', resolvedAt: null }], resolved: [] }
let sources: Source[]
let observer: ReturnType<typeof mockResizeObserver>
let dialog: ReturnType<typeof mockDialog>
let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>

class Source extends EventTarget {
  readyState = 0
  readonly close = vi.fn(() => { this.readyState = 2 })
  constructor() {
    super()
    expect(sources.filter(source => source.readyState !== 2)).toHaveLength(0)
    sources.push(this)
  }
  open() {
    this.readyState = 1
    this.dispatchEvent(new Event('open'))
  }
  status() {
    this.dispatchEvent(new MessageEvent('status.changed', { data: JSON.stringify({ serviceId: 'actual-owner', state: 'down', lastCheckedAt: t2 }) }))
  }
}

beforeEach(() => {
  mockThemeEnvironment()
  observer = mockResizeObserver()
  dialog = mockDialog()
  sources = []
  vi.stubGlobal('EventSource', Source)
  fetchMock = vi.fn<typeof fetch>().mockImplementation(async input => Response.json(String(input).endsWith('/status') ? { services } : feed))
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { dialog.restore(); resetThemeTestState() })

it('runs one synchronization owner through Strict Mode and dashboard interactions', async () => {
  const calculate = vi.spyOn(layout, 'createTreemapLayout')
  const { unmount } = render(<StrictMode><ThemeProvider><App /></ThemeProvider></StrictMode>)
  expect(sources).toHaveLength(2)
  expect(sources[0].close).toHaveBeenCalledOnce()
  expect(fetchMock).not.toHaveBeenCalled()
  await act(async () => sources[1].open())
  expect(fetchMock.mock.calls.map(([url]) => url)).toEqual(['/api/v1/status', '/api/v1/incidents'])
  expect(within(screen.getByRole('status', { name: 'Dashboard connection' })).getByText('Live')).toBeInTheDocument()
  act(() => observer.resize(screen.getByRole('group', { name: 'Service treemap' }), 1000, 600))
  const tile = screen.getByRole('button', { name: 'Live Service: Operational' })
  const geometry = tile.closest('li')?.getAttribute('style')
  const calculations = calculate.mock.calls.length
  act(() => sources[1].status())
  expect(screen.getByRole('button', { name: 'Live Service: Down' })).toBe(tile)
  expect(tile.closest('li')?.getAttribute('style')).toBe(geometry)
  expect(calculate).toHaveBeenCalledTimes(calculations)
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Search services' }))
  await user.type(screen.getByRole('combobox'), 'live')
  await user.keyboard('{Enter}')
  expect(screen.getByRole('dialog', { name: 'Live Service' })).toBeInTheDocument()
  expect(screen.queryByText(/Last checked|Last updated|Latest service check|Demo/)).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Close service details' }))
  await user.click(screen.getByRole('button', { name: 'Incidents (1 active)' }))
  expect(within(screen.getByRole('region', { name: 'Active incidents' })).getByRole('button')).toHaveTextContent('Unknown Service')
  expect(sources).toHaveLength(2)
  expect(fetchMock).toHaveBeenCalledTimes(2)
  unmount()
  expect(sources[1].close).toHaveBeenCalledOnce()
})

it('renders real hook partial success without an incident-empty claim or another owner', async () => {
  fetchMock.mockImplementation(async input => String(input).endsWith('/status') ? Response.json({ services }) : new Response('Unavailable', { status: 503 }))
  const { unmount } = render(<ThemeProvider><App /></ThemeProvider>)
  await act(async () => sources[0].open())
  act(() => observer.resize(screen.getByRole('group', { name: 'Service treemap' }), 1000, 600))
  expect(screen.getByRole('button', { name: 'Live Service: Operational' })).toBeInTheDocument()
  expect(within(screen.getByRole('status', { name: 'Dashboard connection' })).getByText('Reconnecting')).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Incidents' }))
  expect(screen.getByText('Incidents unavailable')).toBeInTheDocument()
  expect(screen.queryByText('No incidents to display')).not.toBeInTheDocument()
  expect(sources).toHaveLength(1)
  unmount()
})
