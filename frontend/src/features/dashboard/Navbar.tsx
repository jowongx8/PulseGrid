import { Activity, Bell, Moon, Search, Sun } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTheme } from '@/theme'

function Clock() {
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    const interval = window.setInterval(() => setNow(new Date()), 1000)
    return () => window.clearInterval(interval)
  }, [])

  return (
    <time
      role="timer"
      aria-label="Current local time"
      aria-live="off"
      dateTime={now.toISOString()}
      className="hidden font-mono text-[11px] text-muted-foreground tabular-nums sm:block"
    >
      {new Intl.DateTimeFormat(undefined, {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hourCycle: 'h23',
      }).format(now)}
    </time>
  )
}

export function Navbar({ activeIncidentCount }: { activeIncidentCount: number }) {
  const { theme, setTheme } = useTheme()

  return (
    <header className="flex h-14 min-w-0 shrink-0 items-center gap-3 border-b bg-card/60 px-4 backdrop-blur-md sm:px-5">
      <div className="flex shrink-0 items-center gap-2">
        <span className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
          <Activity aria-hidden="true" size={16} />
        </span>
        <h1 className="text-sm font-semibold tracking-tight">PulseGrid</h1>
      </div>
      <Clock />
      <span className="flex shrink-0 items-center gap-1 text-[10px] text-muted-foreground">
        <span aria-hidden="true" className="size-1.5 rounded-full bg-muted-foreground" />
        Demo
        <span className="sr-only">: development fixtures; no backend connection</span>
      </span>
      <nav aria-label="Dashboard controls" className="ml-auto flex shrink-0 items-center gap-1.5">
        <button
          type="button"
          disabled
          aria-label="Search services"
          className="flex size-7 items-center justify-center rounded-md text-xs text-muted-foreground disabled:cursor-not-allowed sm:w-auto sm:gap-2 sm:border sm:bg-background sm:px-2.5"
        >
          <Search aria-hidden="true" size={14} />
          <span className="hidden sm:inline">Search services</span>
          <kbd aria-hidden="true" className="hidden rounded border bg-muted px-1 font-mono text-[10px] sm:inline">
            ⌘K
          </kbd>
        </button>
        <button
          type="button"
          disabled
          aria-label={activeIncidentCount > 0 ? `Incidents (${activeIncidentCount} active)` : 'Incidents'}
          className="flex h-7 items-center justify-center gap-1.5 rounded-md border bg-background px-2 text-xs text-muted-foreground disabled:cursor-not-allowed"
        >
          <Bell aria-hidden="true" size={14} />
          <span className="hidden sm:inline">Incidents</span>
          {activeIncidentCount > 0 && (
            <span className="min-w-4 rounded-full bg-down px-1 text-[10px] font-semibold text-background tabular-nums">
              {activeIncidentCount}
            </span>
          )}
        </button>
        <button
          type="button"
          aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          className="flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {theme === 'dark' ? <Sun aria-hidden="true" size={14} /> : <Moon aria-hidden="true" size={14} />}
        </button>
      </nav>
    </header>
  )
}
