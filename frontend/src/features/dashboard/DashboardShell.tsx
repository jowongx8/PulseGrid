import { useCallback, useEffect, useRef, useState } from 'react'
import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { Navbar } from './Navbar'
import { StatusSummary } from './StatusSummary'
import { ServiceTreemap } from './treemap/ServiceTreemap'
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
  services: readonly ServiceDTO[]
  incidents: IncidentsResponse
}

export function DashboardShell({ services, incidents }: DashboardShellProps) {
  const [overlay, setOverlay] = useState<OverlayState>({ kind: 'none' })
  const returnFocus = useRef<HTMLElement | null>(null)
  const searchButton = useRef<HTMLButtonElement>(null)
  const dismiss = useCallback(() => setOverlay({ kind: 'none' }), [])

  function open(next: OverlayState, opener?: HTMLButtonElement) {
    if (overlay.kind === 'none') returnFocus.current = opener ?? searchButton.current
    setOverlay(next)
  }

  function openService(id: string, opener?: HTMLButtonElement) {
    if (services.some((service) => service.id === id)) open({ kind: 'service', serviceId: id }, opener)
  }

  useEffect(() => {
    function shortcut(event: KeyboardEvent) {
      if (!(event.ctrlKey || event.metaKey) || event.altKey || event.key.toLowerCase() !== 'k' || event.repeat || event.isComposing) return
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
  }, [overlay.kind, dismiss])

  const selected = overlay.kind === 'service' ? services.find((service) => service.id === overlay.serviceId) : undefined
  const title = overlay.kind === 'service' ? selected?.name ?? 'Service unavailable'
    : overlay.kind === 'incidents' ? 'Incident feed' : 'Search services'

  return (
    <div className="flex h-dvh min-w-0 flex-col overflow-hidden bg-background">
      <Navbar
        activeIncidentCount={incidents.active.length}
        onSearch={(opener) => open({ kind: 'search' }, opener)}
        onIncidents={(opener) => open({ kind: 'incidents' }, opener)}
        searchButtonRef={searchButton}
      />
      <StatusSummary services={services} />
      <main aria-label="Service map" className="relative min-h-0 min-w-0 flex-1 overflow-hidden">
        <ServiceTreemap services={services} onSelectService={openService} />
      </main>
      <DashboardOverlay
        mode={overlay.kind === 'none' ? null : overlay.kind}
        title={title}
        focusKey={overlay.kind === 'service' ? overlay.serviceId : overlay.kind}
        onDismiss={dismiss}
        returnFocus={returnFocus}
        fallbackFocus={searchButton}
      >
        {overlay.kind === 'search' && <SearchDialog services={services} onSelect={openService} />}
        {overlay.kind === 'service' && <ServiceDetails service={selected} incidents={incidents} />}
        {overlay.kind === 'incidents' && <IncidentsDrawer services={services} incidents={incidents} onSelectService={openService} />}
      </DashboardOverlay>
    </div>
  )
}
