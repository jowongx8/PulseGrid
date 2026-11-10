import { X } from 'lucide-react'
import { useState } from 'react'

const storageKey = 'pulsegrid-desktop-notice-dismissed'

export function MobileNotice({ onDismiss }: { onDismiss: () => void }) {
  const [dismissed, setDismissed] = useState(() => {
    try {
      return localStorage.getItem(storageKey) === 'true'
    } catch {
      return false
    }
  })

  function dismiss() {
    setDismissed(true)
    try {
      localStorage.setItem(storageKey, 'true')
    } catch {
      // The current visit can still dismiss the notice when storage is blocked.
    }
    onDismiss()
  }

  if (dismissed) return null

  return (
    <aside aria-label="Larger-screen notice" className="flex min-w-0 shrink-0 items-start gap-2 border-b bg-card px-3 py-2 md:hidden">
      <div className="min-w-0 flex-1">
        <h2 className="text-xs leading-4 font-semibold">Better on a larger screen</h2>
        <p className="mt-1 text-[11px] leading-4 text-muted-foreground">
          PulseGrid works on mobile, but the service map is easier to explore on a desktop.
        </p>
      </div>
      <button
        type="button"
        aria-label="Dismiss larger-screen notice"
        onClick={dismiss}
        className="flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        <X size={16} aria-hidden="true" />
      </button>
    </aside>
  )
}
