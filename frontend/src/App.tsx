import { useTheme } from '@/theme'

function App() {
  const { theme, setTheme } = useTheme()

  return (
    <main className="mx-auto max-w-lg space-y-5 p-4 sm:p-5">
      <div>
        <h1 className="text-sm font-semibold tracking-tight">PulseGrid</h1>
        <p className="mt-1 text-xs text-muted-foreground">
          Temporary design-system verification.
        </p>
      </div>

      <section aria-labelledby="theme-heading" className="space-y-3">
        <h2
          id="theme-heading"
          className="text-xs font-semibold uppercase tracking-wider text-muted-foreground"
        >
          Theme
        </h2>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            className="rounded-md border bg-background px-2.5 py-1.5 text-xs hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            Switch to {theme === 'dark' ? 'light' : 'dark'}
          </button>
          <span className="text-xs text-muted-foreground" aria-live="polite">
            Current: {theme}
          </span>
        </div>
      </section>

      <section
        aria-labelledby="type-heading"
        className="space-y-3 rounded-md border bg-card p-3 text-card-foreground"
      >
        <h2
          id="type-heading"
          className="text-xs font-semibold uppercase tracking-wider text-muted-foreground"
        >
          Typography
        </h2>
        <p className="text-sm font-semibold tracking-tight">System sans · 14px semibold</p>
        <p className="text-xs font-medium leading-tight">System sans · 12px medium</p>
        <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/80">
          Metadata · 10px semibold
        </p>
        <p className="font-mono text-[11px] tabular-nums text-muted-foreground">
          Monospace · 0123456789
        </p>
      </section>

      <section aria-labelledby="status-heading" className="space-y-3">
        <h2
          id="status-heading"
          className="text-xs font-semibold uppercase tracking-wider text-muted-foreground"
        >
          Status colours
        </h2>
        <div className="space-y-2 text-xs">
          <p className="rounded-md border border-op bg-op-surface px-3 py-2 font-medium text-op-foreground">
            <span className="mr-2 inline-block size-2 rounded-full bg-op" />
            Up <span className="font-normal">— Operational</span>
          </p>
          <p className="rounded-md border border-down bg-down-surface px-3 py-2 font-medium text-down-foreground">
            <span className="mr-2 inline-block size-2 rounded-full bg-down" />
            Down
          </p>
          <p className="rounded-md border border-unknown bg-unknown-surface px-3 py-2 font-medium text-unknown-foreground">
            <span className="mr-2 inline-block size-2 rounded-full bg-unknown" />
            Unknown
          </p>
        </div>
      </section>
    </main>
  )
}

export default App
