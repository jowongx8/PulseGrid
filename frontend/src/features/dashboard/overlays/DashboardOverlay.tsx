import { X } from 'lucide-react'
import { useId, useLayoutEffect, useRef } from 'react'
import type { ReactNode, RefObject } from 'react'
import './overlays.css'

interface DashboardOverlayProps {
  mode: 'search' | 'service' | 'incidents' | null
  title: string
  focusKey: string
  onDismiss: () => void
  returnFocus: RefObject<HTMLElement | null>
  fallbackFocus: RefObject<HTMLButtonElement | null>
  children: ReactNode
}

export function DashboardOverlay({ mode, title, focusKey, onDismiss, returnFocus, fallbackFocus, children }: DashboardOverlayProps) {
  const dialog = useRef<HTMLDialogElement>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const pressedOutside = useRef(false)
  const lastFocused = useRef<HTMLElement | null>(null)
  const titleID = useId()

  useLayoutEffect(() => {
    const element = dialog.current
    if (!element) return
    if (mode === null) {
      if (element.open) element.close()
      return
    }
    if (!element.open) element.showModal()
    const initial = element.querySelector<HTMLElement>('[data-initial-focus]') ?? heading.current
    initial?.focus()
  }, [mode, focusKey])

  useLayoutEffect(() => {
    const element = dialog.current
    if (element?.open && lastFocused.current && !lastFocused.current.isConnected) {
      heading.current?.focus()
    }
  })

  useLayoutEffect(() => {
    const element = dialog.current
    return () => { if (element?.open) element.close() }
  }, [])

  function handleClose() {
    // A queued close from an earlier opening must not dismiss a reopened dialog.
    if (dialog.current?.open) return
    onDismiss()
    const opener = returnFocus.current
    if (opener?.isConnected && !opener.matches(':disabled')) opener.focus()
    // Hidden or removed openers cannot receive focus, including when a desktop
    // tile disappears at the mobile breakpoint. No layout measurement is needed.
    if (document.activeElement !== opener) fallbackFocus.current?.focus()
  }

  return (
    <dialog
      ref={dialog}
      aria-labelledby={titleID}
      data-mode={mode ?? undefined}
      className="dashboard-overlay"
      onClose={handleClose}
      onFocusCapture={(event) => {
        if (event.target instanceof HTMLElement) lastFocused.current = event.target
      }}
      onPointerDown={(event) => { pressedOutside.current = event.button === 0 && event.target === event.currentTarget }}
      onPointerUp={(event) => {
        const dismiss = pressedOutside.current && event.target === event.currentTarget && event.button === 0
        pressedOutside.current = false
        if (dismiss) onDismiss()
      }}
      onPointerCancel={() => { pressedOutside.current = false }}
    >
      <div className="overlay-panel">
        <div className="overlay-header">
          <h2
            ref={heading}
            id={titleID}
            tabIndex={-1}
            title={title}
            className={mode === 'search' ? 'sr-only' : 'min-w-0 truncate text-sm font-semibold focus-visible:outline-2 focus-visible:outline-ring'}
          >
            {title}
          </h2>
          <button
            type="button"
            onClick={onDismiss}
            aria-label={mode === 'service' ? 'Close service details' : mode === 'incidents' ? 'Close incident feed' : 'Close search'}
            className="overlay-close flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            <X size={16} aria-hidden="true" />
          </button>
        </div>
        <div className="overlay-body">{children}</div>
      </div>
    </dialog>
  )
}
