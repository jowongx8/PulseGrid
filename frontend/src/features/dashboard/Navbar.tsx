import { Activity, Bell, Moon, Search, Sun } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import type { Ref } from 'react'
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
      className="hidden font-mono text-[11px] text-muted-foreground tabular-nums md:block"
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

interface NavbarProps {
  activeIncidentCount: number | null
  searchEnabled?: boolean
  onSearch: (opener: HTMLButtonElement) => void
  onIncidents: (opener: HTMLButtonElement) => void
  searchButtonRef?: Ref<HTMLButtonElement>
  incidentsButtonRef?: Ref<HTMLButtonElement>
}

export function Navbar({ activeIncidentCount, searchEnabled = true, onSearch, onIncidents, searchButtonRef, incidentsButtonRef }: NavbarProps) {
  const { theme, setTheme } = useTheme()
  const searchHint = useId()

  return (
    <header className="flex h-14 min-w-0 shrink-0 items-center gap-2 border-b bg-card/60 px-3 backdrop-blur-md md:gap-3 md:px-5">
      <div className="flex shrink-0 items-center gap-2">
        <span className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
          <Activity aria-hidden="true" size={16} />
        </span>
        <h1 className="text-sm font-semibold tracking-tight">PulseGrid</h1>
      </div>
      <Clock />
      <nav aria-label="Dashboard controls" className="ml-auto flex shrink-0 items-center gap-1 md:gap-1.5">
        <button
          type="button"
          ref={searchButtonRef}
          disabled={!searchEnabled}
          aria-describedby={!searchEnabled ? searchHint : undefined}
          onClick={(event) => onSearch(event.currentTarget)}
          aria-haspopup="dialog"
          aria-keyshortcuts="Control+K Meta+K"
          aria-label="Search services"
          className="flex size-8 items-center justify-center rounded-md text-xs text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent md:h-7 md:w-auto md:gap-2 md:border md:bg-background md:px-2.5"
        >
          <Search aria-hidden="true" size={14} />
          <span className="hidden md:inline">Search services</span>
          <kbd aria-hidden="true" className="hidden rounded border bg-muted px-1 font-mono text-[10px] md:inline">
            ⌘K
          </kbd>
        </button>
        {!searchEnabled && <span id={searchHint} className="sr-only">Search will be available when service statuses load.</span>}
        <button
          type="button"
          ref={incidentsButtonRef}
          onClick={(event) => onIncidents(event.currentTarget)}
          aria-haspopup="dialog"
          aria-label={activeIncidentCount !== null && activeIncidentCount > 0 ? `Incidents (${activeIncidentCount} active)` : 'Incidents'}
          className="flex h-8 items-center justify-center gap-1.5 rounded-md border bg-background px-2 text-xs text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:h-7"
        >
          <Bell aria-hidden="true" size={14} />
          <span className="hidden md:inline">Incidents</span>
          {activeIncidentCount !== null && activeIncidentCount > 0 && (
            <span className="min-w-4 rounded-full bg-down px-1 text-[10px] font-semibold text-background tabular-nums">
              {activeIncidentCount}
            </span>
          )}
        </button>
        <button
          type="button"
          aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          className="flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:size-7"
        >
          {theme === 'dark' ? <Sun aria-hidden="true" size={14} /> : <Moon aria-hidden="true" size={14} />}
        </button>
      </nav>
    </header>
  )
}
