import { useCallback, useEffect, useRef, useState } from 'react'
import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { Navbar } from './Navbar'
import { StatusSummary } from './StatusSummary'
import { MobileNotice } from './MobileNotice'
import { ServiceTreemap } from './treemap/ServiceTreemap'
import { TreemapSkeleton } from './treemap/TreemapSkeleton'
import type { ConnectionStatus } from './live-presentation'
import { DashboardOverlay } from './overlays/DashboardOverlay'
import { SearchDialog } from './overlays/SearchDialog'
import { ServiceDetails } from './overlays/ServiceDetails'
import { IncidentsDrawer } from './overlays/IncidentsDrawer'

type OverlayState =
  | { kind: 'none' }
  | { kind: 'search' }
  | { kind: 'service'; serviceId: string }
  | { kind: 'incidents' }

interface DashboardShellProps {
  services: readonly ServiceDTO[] | null
  incidents: IncidentsResponse | null
  connectionStatus: ConnectionStatus
  servicesUnavailable?: boolean
  incidentsUnavailable?: boolean
}

export function DashboardShell({ services, incidents, connectionStatus, servicesUnavailable = false, incidentsUnavailable = false }: DashboardShellProps) {
  const [overlay, setOverlay] = useState<OverlayState>({ kind: 'none' })
  const returnFocus = useRef<HTMLElement | null>(null)
  const searchButton = useRef<HTMLButtonElement>(null)
  const incidentsButton = useRef<HTMLButtonElement>(null)
  const searchEnabled = services !== null
  const fallbackFocus = searchEnabled ? searchButton : incidentsButton
  const dismiss = useCallback(() => setOverlay({ kind: 'none' }), [])

  function open(next: OverlayState, opener?: HTMLButtonElement) {
    if (overlay.kind === 'none') returnFocus.current = opener ?? searchButton.current
    setOverlay(next)
  }

  function openService(id: string, opener?: HTMLButtonElement) {
    if (services?.some((service) => service.id === id)) open({ kind: 'service', serviceId: id }, opener)
  }

  useEffect(() => {
    function shortcut(event: KeyboardEvent) {
      if (!(event.ctrlKey || event.metaKey) || event.altKey || event.key.toLowerCase() !== 'k' || event.repeat || event.isComposing) return
      if (!searchEnabled) return
      event.preventDefault()
      if (overlay.kind === 'search') {
        dismiss()
      } else {
        if (overlay.kind === 'none') {
          returnFocus.current = document.activeElement instanceof HTMLElement && document.activeElement !== document.body
            ? document.activeElement : searchButton.current
        }
        setOverlay({ kind: 'search' })
      }
    }
    document.addEventListener('keydown', shortcut)
    return () => document.removeEventListener('keydown', shortcut)
  }, [overlay.kind, dismiss, searchEnabled])

  const selected = overlay.kind === 'service' ? services?.find((service) => service.id === overlay.serviceId) : undefined
  if (overlay.kind === 'service' && !selected) setOverlay({ kind: 'none' })
  const title = overlay.kind === 'service' ? selected?.name ?? 'Service unavailable'
    : overlay.kind === 'incidents' ? 'Incident feed' : 'Search services'

  return (
    <div className="flex min-h-dvh min-w-0 flex-col bg-background pt-[env(safe-area-inset-top)] pr-[env(safe-area-inset-right)] pb-[env(safe-area-inset-bottom)] pl-[env(safe-area-inset-left)]">
      <Navbar
        activeIncidentCount={incidents?.active.length ?? null}
        searchEnabled={searchEnabled}
        onSearch={(opener) => open({ kind: 'search' }, opener)}
        onIncidents={(opener) => open({ kind: 'incidents' }, opener)}
        searchButtonRef={searchButton}
        incidentsButtonRef={incidentsButton}
      />
      <StatusSummary services={services} connectionStatus={connectionStatus} unavailable={servicesUnavailable} />
      <MobileNotice onDismiss={() => fallbackFocus.current?.focus()} />
      <main aria-label="Service map" aria-busy={services === null && !servicesUnavailable} className="relative min-h-40 min-w-0 flex-1 overflow-hidden">
        {services === null ? servicesUnavailable ? (
          <div role="status" className="absolute inset-0 flex flex-col items-center justify-center gap-2 p-4 text-center text-xs text-muted-foreground">
            <h2 className="font-semibold">Unable to load service statuses</h2>
            <p>We're having trouble loading the dashboard. Retrying automatically.</p>
          </div>
        ) : <TreemapSkeleton /> : services.length === 0 ? (
          <div role="status" className="absolute inset-0 flex flex-col items-center justify-center gap-2 p-4 text-center text-xs text-muted-foreground">
            <h2 className="font-semibold">No services available</h2>
            <p>There are currently no services to display.</p>
          </div>
        ) : <ServiceTreemap services={services} onSelectService={openService} />}
      </main>
      <DashboardOverlay
        mode={overlay.kind === 'none' ? null : overlay.kind}
        title={title}
        focusKey={overlay.kind === 'service' ? overlay.serviceId : overlay.kind}
        onDismiss={dismiss}
        returnFocus={returnFocus}
        fallbackFocus={fallbackFocus}
      >
        {overlay.kind === 'search' && services !== null && <SearchDialog services={services} onSelect={openService} />}
        {overlay.kind === 'service' && <ServiceDetails service={selected} incidents={incidents} incidentsUnavailable={incidentsUnavailable} />}
        {overlay.kind === 'incidents' && <IncidentsDrawer services={services} incidents={incidents} unavailable={incidentsUnavailable} stale={connectionStatus !== 'live'} onSelectService={openService} />}
      </DashboardOverlay>
    </div>
  )
}
