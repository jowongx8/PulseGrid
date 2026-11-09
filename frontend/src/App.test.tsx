import { render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it } from 'vitest'
import App from '@/App'
import { ThemeProvider } from '@/ThemeProvider'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'

beforeEach(() => {
  mockThemeEnvironment()
})
afterEach(resetThemeTestState)

it('supplies the development fixtures to the dashboard', () => {
  render(<ThemeProvider><App /></ThemeProvider>)

  expect(screen.getByRole('heading', { name: 'PulseGrid', level: 1 })).toBeInTheDocument()
  expect(screen.getByRole('main', { name: 'Service map' })).toBeInTheDocument()
  const summary = within(screen.getByRole('region', { name: 'Service status summary' }))
  expect(summary.getByRole('heading', { name: '1 service down' })).toBeInTheDocument()
  expect(summary.getByText('70% healthy · 10 services monitored')).toBeInTheDocument()
  expect(summary.getAllByRole('listitem').map((chip) => chip.textContent)).toEqual([
    '1Down', '7Operational', '2Unknown',
  ])
  expect(screen.getByRole('button', { name: 'Incidents (1 active)' })).toBeDisabled()
  expect(screen.queryByText(/Temporary design-system verification/)).not.toBeInTheDocument()
})
