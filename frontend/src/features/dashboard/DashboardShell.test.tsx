import { act, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ThemeProvider } from '@/ThemeProvider'
import { makeService } from '@/test/services'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'
import { DashboardShell } from './DashboardShell'
import { mockResizeObserver } from '@/test/resize-observer'
import * as layout from './treemap/layout'

let observer: ReturnType<typeof mockResizeObserver>
beforeEach(() => {
  mockThemeEnvironment()
  observer = mockResizeObserver()
})
afterEach(resetThemeTestState)

it('keeps the content host mounted when supplied services and incidents change', () => {
  const calculate = vi.spyOn(layout, 'createTreemapLayout')
  const { rerender } = render(
    <ThemeProvider>
      <DashboardShell services={[makeService('new-service', 'up')]} activeIncidents={[]} />
    </ThemeProvider>,
  )
  const main = screen.getByRole('main', { name: 'Service map' })
  act(() => observer.resize(within(main).getByRole('group', { name: 'Service treemap' }), 800, 500))
  const tile = within(main).getByRole('listitem', { name: 'Service new-service: Operational' })
  const calls = calculate.mock.calls.length
  expect(screen.getByRole('heading', { name: 'All monitored services operational' })).toBeInTheDocument()

  rerender(
    <ThemeProvider>
      <DashboardShell
        services={[makeService('new-service', 'unknown')]}
        activeIncidents={[{ serviceId: 'new-service', startedAt: '2026-09-24T12:00:00.123456Z', resolvedAt: null }]}
      />
    </ThemeProvider>,
  )
  expect(screen.getByRole('main', { name: 'Service map' })).toBe(main)
  expect(screen.getByRole('heading', { name: '1 service status unknown' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Incidents (1 active)' })).toBeDisabled()
  expect(within(main).getByRole('listitem', { name: 'Service new-service: Unknown' })).toBe(tile)
  expect(calculate).toHaveBeenCalledTimes(calls)
})
