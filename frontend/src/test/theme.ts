import { vi } from 'vitest'
import type { Theme } from '@/theme'

export function resetThemeTestState() {
  localStorage.clear()
  document.documentElement.classList.remove('light', 'dark')
  document.querySelector('meta[name="theme-color"]')?.remove()
}

export function mockThemeEnvironment(initialTheme: Theme = 'light') {
  resetThemeTestState()
  const meta = document.createElement('meta')
  meta.name = 'theme-color'
  document.head.append(meta)

  let matches = initialTheme === 'dark'
  const events = new EventTarget()
  const media = {
    media: '(prefers-color-scheme: dark)',
    get matches() {
      return matches
    },
    addEventListener: vi.fn(events.addEventListener.bind(events)),
    removeEventListener: vi.fn(events.removeEventListener.bind(events)),
  }

  vi.stubGlobal('matchMedia', vi.fn((query: string) => {
    if (query !== media.media) {
      throw new Error(`Unexpected theme media query: ${query}`)
    }
    return media
  }))

  return {
    media,
    setSystemTheme(theme: Theme) {
      const nextMatches = theme === 'dark'
      if (matches === nextMatches) return
      matches = nextMatches
      const event = Object.assign(new Event('change'), { matches, media: media.media })
      events.dispatchEvent(event)
    },
  }
}
