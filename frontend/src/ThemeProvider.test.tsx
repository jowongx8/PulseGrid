import { StrictMode } from 'react'
import { act, render, renderHook, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider } from '@/ThemeProvider'
import { useTheme } from '@/theme'
import type { Theme } from '@/theme'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'

function ThemeProbe() {
  const { theme, setTheme } = useTheme()
  return (
    <>
      <p role="status">{theme}</p>
      <button onClick={() => setTheme('light')}>Select light</button>
      <button onClick={() => setTheme('dark')}>Select dark</button>
    </>
  )
}

function renderTheme() {
  return render(
    <StrictMode>
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>
    </StrictMode>,
  )
}

function expectTheme(theme: Theme) {
  expect(screen.getByRole('status')).toHaveTextContent(new RegExp(`^${theme}$`))
  expect(document.documentElement).toHaveClass(theme)
  expect(document.documentElement).not.toHaveClass(theme === 'light' ? 'dark' : 'light')
  expect(document.querySelector('meta[name="theme-color"]')).toHaveAttribute(
    'content',
    theme === 'dark' ? '#1a1b1e' : '#f7f7f8',
  )
}

describe('ThemeProvider', () => {
  let environment: ReturnType<typeof mockThemeEnvironment>

  beforeEach(() => {
    environment = mockThemeEnvironment()
  })

  afterEach(resetThemeTestState)

  it.each([
    { saved: null, system: 'light', expected: 'light' },
    { saved: null, system: 'dark', expected: 'dark' },
    { saved: 'light', system: 'dark', expected: 'light' },
    { saved: 'dark', system: 'light', expected: 'dark' },
    { saved: 'invalid', system: 'light', expected: 'light' },
    { saved: 'invalid', system: 'dark', expected: 'dark' },
  ] as const)('resolves saved=$saved and system=$system to $expected', ({ saved, system, expected }) => {
    environment.setSystemTheme(system)
    if (saved !== null) localStorage.setItem('pulsegrid-theme', saved)

    renderTheme()

    expectTheme(expected)
    expect(localStorage.getItem('pulsegrid-theme')).toBe(saved)
  })

  it.each(['light', 'dark'] as const)('selects and persists %s', async (theme) => {
    environment.setSystemTheme(theme === 'light' ? 'dark' : 'light')
    const user = userEvent.setup()
    renderTheme()

    await user.click(screen.getByRole('button', { name: `Select ${theme}` }))

    expectTheme(theme)
    expect(localStorage.getItem('pulsegrid-theme')).toBe(theme)
  })

  it('restores the explicit choice after remounting', async () => {
    const user = userEvent.setup()
    const first = renderTheme()
    await user.click(screen.getByRole('button', { name: 'Select dark' }))
    first.unmount()

    renderTheme()

    expectTheme('dark')
    expect(localStorage.getItem('pulsegrid-theme')).toBe('dark')
  })

  it('follows system changes when no explicit preference exists', () => {
    renderTheme()
    expectTheme('light')

    act(() => environment.setSystemTheme('dark'))
    expectTheme('dark')

    act(() => environment.setSystemTheme('light'))
    expectTheme('light')
    expect(localStorage.getItem('pulsegrid-theme')).toBeNull()
  })

  it('keeps an explicit choice when the system preference changes', async () => {
    const user = userEvent.setup()
    renderTheme()
    await user.click(screen.getByRole('button', { name: 'Select dark' }))

    act(() => environment.setSystemTheme('dark'))
    act(() => environment.setSystemTheme('light'))

    expectTheme('dark')
    expect(localStorage.getItem('pulsegrid-theme')).toBe('dark')
  })

  it('removes media query subscriptions when unmounted', () => {
    const view = renderTheme()
    const registrations = environment.media.addEventListener.mock.calls
    expect(registrations.length).toBeGreaterThan(0)

    view.unmount()

    expect(environment.media.removeEventListener).toHaveBeenCalledTimes(registrations.length)
    for (const [event, listener] of registrations) {
      expect(environment.media.removeEventListener).toHaveBeenCalledWith(event, listener)
    }
  })

  it('falls back to the system when localStorage reads fail', () => {
    environment.setSystemTheme('dark')
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('Storage unavailable', 'SecurityError')
    })

    renderTheme()

    expectTheme('dark')
  })

  it('continues switching for the session when localStorage writes fail', async () => {
    const write = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('Storage unavailable', 'SecurityError')
    })
    const user = userEvent.setup()
    renderTheme()

    await user.click(screen.getByRole('button', { name: 'Select dark' }))
    expectTheme('dark')
    expect(write).toHaveBeenCalledWith('pulsegrid-theme', 'dark')

    act(() => environment.setSystemTheme('dark'))
    act(() => environment.setSystemTheme('light'))
    expectTheme('dark')

    await user.click(screen.getByRole('button', { name: 'Select light' }))
    expectTheme('light')
    expect(write).toHaveBeenCalledWith('pulsegrid-theme', 'light')
    expect(localStorage.getItem('pulsegrid-theme')).toBeNull()
  })

  it('reports useTheme outside its provider', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})

    expect(() => renderHook(() => useTheme())).toThrow(
      'useTheme must be used within ThemeProvider',
    )
  })
})
