import { act, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from '@/App'
import { ThemeProvider } from '@/ThemeProvider'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'
import { mockResizeObserver } from '@/test/resize-observer'

let observer: ReturnType<typeof mockResizeObserver>
beforeEach(() => {
  mockThemeEnvironment()
  observer = mockResizeObserver()
})
afterEach(resetThemeTestState)

it('supplies the development fixtures to the dashboard', () => {
  const fetch = vi.fn()
  vi.stubGlobal('fetch', fetch)
  render(<ThemeProvider><App /></ThemeProvider>)

  expect(screen.getByRole('heading', { name: 'PulseGrid', level: 1 })).toBeInTheDocument()
  expect(screen.getByRole('main', { name: 'Service map' })).toBeInTheDocument()
  const summary = within(screen.getByRole('region', { name: 'Service status summary' }))
  expect(summary.getByRole('heading', { name: '1 service down' })).toBeInTheDocument()
  expect(summary.getByText('70% healthy · 10 services monitored')).toBeInTheDocument()
  expect(summary.getAllByRole('listitem').map((chip) => chip.textContent)).toEqual([
    '1Down', '7Operational', '2Unknown',
  ])
  expect(screen.getByRole('button', { name: 'Incidents (1 active)' })).toBeEnabled()
  expect(screen.queryByText(/Temporary design-system verification/)).not.toBeInTheDocument()
  const treemap = screen.getByRole('group', { name: 'Service treemap' })
  act(() => observer.resize(treemap, 1000, 600))
  expect(within(treemap).getAllByRole('listitem')).toHaveLength(10)
  expect(within(treemap).getByRole('button', { name: 'GitHub: Down' })).toBeInTheDocument()
  expect(within(treemap).getByRole('button', { name: 'Discord: Unknown' })).toBeInTheDocument()
  expect(fetch).not.toHaveBeenCalled()
})
