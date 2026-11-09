import { render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { ThemeProvider } from '@/ThemeProvider'
import { makeService } from '@/test/services'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'
import { DashboardShell } from './DashboardShell'

beforeEach(() => {
  mockThemeEnvironment()
})
afterEach(resetThemeTestState)

it('keeps the content host mounted when supplied services and incidents change', () => {
  const { rerender } = render(
    <ThemeProvider>
      <DashboardShell services={[makeService('new-service', 'up')]} activeIncidents={[]} />
    </ThemeProvider>,
  )
  const main = screen.getByRole('main', { name: 'Service map' })
  expect(within(main).getByText('Service map coming soon.')).toBeInTheDocument()
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
})
