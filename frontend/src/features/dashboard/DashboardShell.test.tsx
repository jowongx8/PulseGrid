import { StrictMode, type ComponentProps } from 'react'
import userEvent from '@testing-library/user-event'
import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { mockDialog } from '@/test/dialog'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ThemeProvider } from '@/ThemeProvider'
import { makeService } from '@/test/services'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'
import { DashboardShell } from './DashboardShell'
import { mockResizeObserver } from '@/test/resize-observer'
import * as layout from './treemap/layout'

let observer: ReturnType<typeof mockResizeObserver>
let native: ReturnType<typeof mockDialog>
beforeEach(() => {
  mockThemeEnvironment()
  observer = mockResizeObserver()
  native = mockDialog()
})
afterEach(() => { native.restore(); resetThemeTestState() })

it('keeps the content host mounted when supplied services and incidents change', () => {
  const calculate = vi.spyOn(layout, 'createTreemapLayout')
  const { rerender } = render(
    <ThemeProvider>
      <DashboardShell connectionStatus="live" services={[makeService('new-service', 'up')]} incidents={{ active: [], resolved: [] }} />
    </ThemeProvider>,
  )
  const main = screen.getByRole('main', { name: 'Service map' })
  act(() => observer.resize(within(main).getByRole('group', { name: 'Service treemap' }), 800, 500))
  const tile = within(main).getByRole('button', { name: 'Service new-service: Operational' })
  const calls = calculate.mock.calls.length
  expect(screen.getByRole('heading', { name: 'All monitored services operational' })).toBeInTheDocument()

  rerender(
    <ThemeProvider>
      <DashboardShell
        connectionStatus="live"
        services={[makeService('new-service', 'unknown')]}
        incidents={{ active: [{ serviceId: 'new-service', startedAt: '2026-09-24T12:00:00.123456Z', resolvedAt: null }], resolved: [] }}
      />
    </ThemeProvider>,
  )
  expect(screen.getByRole('main', { name: 'Service map' })).toBe(main)
  expect(screen.getByRole('heading', { name: '1 service status unknown' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Incidents (1 active)' })).toBeEnabled()
  expect(within(main).getByRole('button', { name: 'Service new-service: Unknown' })).toBe(tile)
  expect(calculate).toHaveBeenCalledTimes(calls)
})

it('dismisses the notice without replacing the map and restores focus to Search', async () => {
  localStorage.removeItem('pulsegrid-desktop-notice-dismissed')
  render(shell())
  measure()
  const map = screen.getByRole('group', { name: 'Service treemap' })
  await userEvent.setup().click(screen.getByRole('button', { name: 'Dismiss larger-screen notice' }))
  expect(screen.queryByRole('complementary', { name: 'Larger-screen notice' })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Search services' })).toHaveFocus()
  expect(screen.getByRole('group', { name: 'Service treemap' })).toBe(map)
})

const alpha = { ...makeService('a', 'up'), name: 'Alpha', category: 'developer-cloud' as const }
const beta = { ...makeService('b', 'unknown'), name: 'Beta' }
const empty: IncidentsResponse = { active: [], resolved: [] }
const shell = (services: readonly ServiceDTO[] | null = [alpha, beta], incidents: IncidentsResponse | null = empty, options: Partial<ComponentProps<typeof DashboardShell>> = {}) => (
  <ThemeProvider><DashboardShell connectionStatus="live" services={services} incidents={incidents} {...options} /></ThemeProvider>
)

function measure() {
  act(() => observer.resize(screen.getByRole('group', { name: 'Service treemap' }), 1000, 600))
}

it('keeps the shell and bell available while loading and ignores disabled search shortcuts', async () => {
  const user = userEvent.setup()
  const { rerender } = render(shell(null, null, { connectionStatus: 'connecting' }))
  const main = screen.getByRole('main', { name: 'Service map' })
  expect(main).toHaveAttribute('aria-busy', 'true')
  expect(screen.getByRole('status', { name: 'Loading service statuses' })).toBeInTheDocument()
  expect(within(main).queryByRole('button')).not.toBeInTheDocument()
  await user.keyboard('{Control>}k{/Control}')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Incidents' }))
  expect(screen.getByRole('status', { name: 'Loading incidents' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Close incident feed' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Incidents' })).toHaveFocus())
  rerender(shell([], empty))
  expect(screen.getByRole('main', { name: 'Service map' })).toBe(main)
  expect(screen.getByRole('heading', { name: 'No services available' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Search services' })).toBeEnabled()
})

it('uses the bell as the notice-dismissal focus fallback while search is disabled', async () => {
  localStorage.removeItem('pulsegrid-desktop-notice-dismissed')
  render(shell(null, null, { connectionStatus: 'connecting' }))
  await userEvent.setup().click(screen.getByRole('button', { name: 'Dismiss larger-screen notice' }))
  expect(screen.getByRole('button', { name: 'Incidents' })).toHaveFocus()
})

it('transitions Search directly to details in one native dialog and restores Search focus', async () => {
  const user = userEvent.setup()
  const calculate = vi.spyOn(layout, 'createTreemapLayout')
  render(shell())
  measure()
  const calls = calculate.mock.calls.length
  const search = screen.getByRole('button', { name: 'Search services' })
  await user.click(search)
  const dialog = screen.getByRole('dialog', { name: 'Search services' })
  const input = within(dialog).getByRole('combobox')
  expect(input).toHaveFocus()
  await user.type(input, '  BETA ')
  await user.keyboard('{Enter}')
  expect(screen.getByRole('dialog', { name: 'Beta' })).toBe(dialog)
  expect(screen.getByRole('heading', { name: 'Beta' })).toHaveFocus()
  expect(within(dialog).getByText('Unknown')).toBeInTheDocument()
  expect(native.showModal).toHaveBeenCalledOnce()
  expect(native.close).not.toHaveBeenCalled()
  expect(calculate).toHaveBeenCalledTimes(calls)
  await user.click(screen.getByRole('button', { name: 'Close service details' }))
  await waitFor(() => expect(search).toHaveFocus())
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

it('shows active-only badge counts and moves from incidents directly to service details', async () => {
  const user = userEvent.setup()
  const feed: IncidentsResponse = {
    active: [{ serviceId: 'b', startedAt: '2026-11-10T12:00:00.123456Z', resolvedAt: null }],
    resolved: [{ serviceId: 'a', startedAt: '2026-11-10T10:00:00Z', resolvedAt: '2026-11-10T11:00:00Z' }],
  }
  render(shell([alpha, beta], feed))
  const bell = screen.getByRole('button', { name: 'Incidents (1 active)' })
  await user.click(bell)
  const dialog = screen.getByRole('dialog', { name: 'Incident feed' })
  expect(screen.getByRole('heading', { name: 'Incident feed' })).toHaveFocus()
  await user.click(within(screen.getByRole('region', { name: 'Active incidents' })).getByRole('button', { name: /Beta/ }))
  expect(screen.getByRole('dialog', { name: 'Beta' })).toBe(dialog)
  expect(within(dialog).getByText('Unknown')).toBeInTheDocument()
  expect(within(dialog).getByText('Ongoing')).toBeInTheDocument()
  expect(native.showModal).toHaveBeenCalledOnce()
  await user.click(screen.getByRole('button', { name: 'Close service details' }))
  await waitFor(() => expect(bell).toHaveFocus())
})

it('opens a tile with Enter, keeps its geometry and returns focus to that tile', async () => {
  const user = userEvent.setup()
  render(shell())
  measure()
  const tile = screen.getByRole('button', { name: 'Alpha: Operational' })
  const style = tile.closest('li')?.getAttribute('style')
  tile.focus()
  await user.keyboard('{Enter}')
  expect(screen.getByRole('dialog', { name: 'Alpha' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Close service details' }))
  await waitFor(() => expect(tile).toHaveFocus())
  expect(tile.closest('li')?.getAttribute('style')).toBe(style)
})

it('resolves latest props by ID and closes removed service details with a fallback opener', async () => {
  const user = userEvent.setup()
  const { rerender } = render(shell())
  measure()
  await user.click(screen.getByRole('button', { name: 'Alpha: Operational' }))
  const dialog = screen.getByRole('dialog')
  const time = '2026-11-10T20:00:00.123456789+08:00'
  rerender(shell([{ ...alpha, name: 'Renamed Alpha', status: { state: 'down', lastCheckedAt: time } }, beta], {
    active: [], resolved: [{ serviceId: 'a', startedAt: '2026-11-10T10:00:00Z', resolvedAt: '2026-11-10T11:00:00.000001Z' }],
  }))
  expect(screen.getByRole('dialog', { name: 'Renamed Alpha' })).toBe(dialog)
  expect(within(dialog).getByText('Down')).toBeInTheDocument()
  expect(dialog.querySelector('time')).toHaveAttribute('datetime', '2026-11-10T10:00:00Z')
  expect(screen.queryByText('Last checked')).not.toBeInTheDocument()
  expect(within(dialog).getByText('Resolved')).toBeInTheDocument()
  rerender(shell([beta]))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  await waitFor(() => expect(screen.getByRole('button', { name: 'Search services' })).toHaveFocus())
})

it('toggles Search by shortcut, replaces other overlays, and handles native Escape dismissal', async () => {
  const user = userEvent.setup()
  render(shell())
  const bell = screen.getByRole('button', { name: 'Incidents' })
  await user.click(bell)
  const dialog = screen.getByRole('dialog') as HTMLDialogElement
  await user.keyboard('{Control>}k{/Control}')
  expect(screen.getByRole('dialog', { name: 'Search services' })).toBe(dialog)
  expect(native.showModal).toHaveBeenCalledOnce()
  await user.keyboard('{Control>}k{/Control}')
  await waitFor(() => expect(bell).toHaveFocus())
  await user.keyboard('{Meta>}k{/Meta}')
  expect(screen.getByRole('combobox')).toHaveFocus()
  act(() => native.cancel(dialog))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})

it('dismisses only deliberate backdrop clicks, not content clicks or drags from inside', async () => {
  const user = userEvent.setup()
  render(shell())
  await user.click(screen.getByRole('button', { name: 'Search services' }))
  const dialog = screen.getByRole('dialog')
  const panel = dialog.querySelector('.overlay-panel')!
  await user.click(within(dialog).getByRole('status'))
  expect(dialog).toHaveAttribute('open')
  await user.pointer([{ target: panel, keys: '[MouseLeft>]' }, { target: dialog, keys: '[/MouseLeft]' }])
  expect(dialog).toHaveAttribute('open')
  await user.pointer({ target: dialog, keys: '[MouseLeft]' })
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})

it('keeps unsupported-category services searchable without creating a partial map', async () => {
  const user = userEvent.setup()
  const unsupported = { ...beta, category: 'future-category' } as unknown as ServiceDTO
  render(shell([alpha, unsupported]))
  measure()
  expect(screen.getByText('Service map unavailable: unsupported categories. Use search to view services.')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Alpha: Operational' })).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Search services' }))
  await user.type(screen.getByRole('combobox'), 'beta')
  await user.keyboard('{Enter}')
  expect(screen.getByRole('dialog', { name: 'Beta' })).toBeInTheDocument()
  expect(within(screen.getByRole('dialog')).getByText('Unsupported category (future-category)')).toBeInTheDocument()
})

it('cleans up the shortcut and native dialog in Strict Mode without changing page scroll styles', async () => {
  const user = userEvent.setup()
  const add = vi.spyOn(document, 'addEventListener')
  const remove = vi.spyOn(document, 'removeEventListener')
  const overflow = document.body.style.overflow
  const { unmount } = render(<StrictMode>{shell()}</StrictMode>)
  await user.click(screen.getByRole('button', { name: 'Search services' }))
  const listeners = add.mock.calls.filter(([name]) => name === 'keydown')
  unmount()
  for (const [, handler] of listeners) expect(remove).toHaveBeenCalledWith('keydown', handler)
  expect(native.close).toHaveBeenCalled()
  expect(document.body.style.overflow).toBe(overflow)
})
