import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider } from '@/ThemeProvider'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'
import { Navbar } from './Navbar'

beforeEach(() => {
  mockThemeEnvironment()
})

afterEach(() => {
  vi.useRealTimers()
  resetThemeTestState()
})

const onSearch = vi.fn()
const onIncidents = vi.fn()

function renderNavbar(activeIncidentCount: number | null = 0, searchEnabled = true) {
  return render(<ThemeProvider><Navbar onSearch={onSearch} onIncidents={onIncidents} activeIncidentCount={activeIncidentCount} searchEnabled={searchEnabled} /></ThemeProvider>)
}

describe('Navbar', () => {
  it('renders accessible controls without a demo indicator', async () => {
    const user = userEvent.setup()
    renderNavbar(2)
    expect(screen.getByRole('banner')).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Dashboard controls' })).toBeInTheDocument()
    const search = screen.getByRole('button', { name: 'Search services' })
    const incidents = screen.getByRole('button', { name: 'Incidents (2 active)' })
    expect(search).toBeEnabled()
    expect(incidents).toBeEnabled()
    expect(within(incidents).getByText('2')).toBeInTheDocument()
    expect(screen.queryByText(/Demo|development fixtures/)).not.toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()

    await user.click(search)
    await user.click(incidents)
    expect(onSearch).toHaveBeenCalledWith(search)
    expect(onIncidents).toHaveBeenCalledWith(incidents)
  })

  it.each([null, 0])('shows a normal enabled bell for an active count of %s', count => {
    renderNavbar(count)
    const incidents = screen.getByRole('button', { name: 'Incidents' })
    expect(within(incidents).queryByText('0')).not.toBeInTheDocument()
    expect(incidents).toBeEnabled()
  })

  it('disables only search before catalogue availability and explains why', async () => {
    const user = userEvent.setup()
    renderNavbar(null, false)
    const search = screen.getByRole('button', { name: 'Search services' })
    expect(search).toBeDisabled()
    expect(search).toHaveAccessibleDescription('Search will be available when service statuses load.')
    await user.click(search)
    expect(onSearch).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Incidents' }))
    expect(onIncidents).toHaveBeenCalledOnce()
  })

  it.each(['light', 'dark'] as const)('switches from %s by keyboard and persists the choice', async (initial) => {
    localStorage.setItem('pulsegrid-theme', initial)
    const next = initial === 'light' ? 'dark' : 'light'
    const user = userEvent.setup()
    renderNavbar()

    await user.tab()
    await user.tab()
    await user.tab()
    const toggle = screen.getByRole('button', { name: `Switch to ${next} theme` })
    expect(toggle).toHaveFocus()
    await user.keyboard('{Enter}')

    expect(document.documentElement).toHaveClass(next)
    expect(localStorage.getItem('pulsegrid-theme')).toBe(next)
    expect(screen.getByRole('button', { name: `Switch to ${initial} theme` })).toHaveFocus()
  })

  it('ticks an independent wall clock and clears its interval on unmount', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-07T12:34:56Z'))
    const { unmount } = renderNavbar()
    const clock = screen.getByRole('timer', { name: 'Current local time' })
    const previousText = clock.textContent
    expect(clock).toHaveAttribute('datetime', '2026-11-07T12:34:56.000Z')
    expect(clock).toHaveAttribute('aria-live', 'off')

    act(() => vi.advanceTimersByTime(1000))
    expect(clock).toHaveAttribute('datetime', '2026-11-07T12:34:57.000Z')
    expect(clock.textContent).not.toBe(previousText)
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
