import { useLayoutEffect, useState, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import { ThemeContext } from '@/theme'
import type { Theme } from '@/theme'

// Keep the storage key and initial theme resolution in sync with index.html.
const storageKey = 'pulsegrid-theme'
const systemQuery = '(prefers-color-scheme: dark)'

function readPreference(): Theme | null {
  try {
    const saved = localStorage.getItem(storageKey)
    return saved === 'light' || saved === 'dark' ? saved : null
  } catch {
    // Storage can be unavailable; system mode and in-memory switching still work.
    return null
  }
}

function getSystemTheme(): Theme {
  return window.matchMedia(systemQuery).matches ? 'dark' : 'light'
}

function subscribeToSystemTheme(onChange: () => void) {
  const media = window.matchMedia(systemQuery)
  media.addEventListener('change', onChange)
  return () => media.removeEventListener('change', onChange)
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState(readPreference)
  const systemTheme = useSyncExternalStore(subscribeToSystemTheme, getSystemTheme)
  const theme = preference ?? systemTheme

  useLayoutEffect(() => {
    const root = document.documentElement
    root.classList.remove('light', 'dark')
    root.classList.add(theme)
    document.querySelector('meta[name="theme-color"]')?.setAttribute(
      'content',
      theme === 'dark' ? '#1a1b1e' : '#f7f7f8',
    )
  }, [theme])

  function setTheme(nextTheme: Theme) {
    setPreference(nextTheme)
    try {
      localStorage.setItem(storageKey, nextTheme)
    } catch {
      // Keep the explicit choice for this session even if it cannot be saved.
    }
  }

  return (
    <ThemeContext value={{ theme, setTheme }}>
      {children}
    </ThemeContext>
  )
}
