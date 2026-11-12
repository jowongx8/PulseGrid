import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { IncidentHistory } from './IncidentHistory'

interface IncidentsDrawerProps {
  incidents: IncidentsResponse | null
  services: readonly ServiceDTO[] | null
  onSelectService: (id: string) => void
  unavailable?: boolean
  stale?: boolean
}

export function IncidentsDrawer({ incidents, services, onSelectService, unavailable = false, stale = false }: IncidentsDrawerProps) {
  if (incidents === null) {
    return unavailable ? (
      <div role="status" className="space-y-2 p-4 text-xs text-muted-foreground">
        <h3 className="font-semibold">Incidents unavailable</h3>
        <p>Unable to load incident information. We'll retry automatically.</p>
      </div>
    ) : (
      <div role="status" aria-label="Loading incidents" aria-busy="true" className="space-y-3 p-4">
        <span className="sr-only">Loading incidents…</span>
        <div aria-hidden="true" className="space-y-3 motion-safe:animate-pulse">
          {[0, 1, 2].map(index => <div key={index} className="space-y-3 rounded-md border bg-background p-3"><div className="h-3 w-2/3 rounded-sm bg-muted" /><div className="h-2 w-4/5 rounded-sm bg-muted" /></div>)}
        </div>
      </div>
    )
  }
  return (
    <div className="space-y-3 p-4">
      {stale && <p className="text-[11px] text-muted-foreground">Showing last-known incident information.</p>}
      {incidents.active.length === 0 && incidents.resolved.length === 0
        ? <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">No incidents to display</p>
        : <IncidentHistory incidents={incidents} services={services} onSelectService={onSelectService} />}
    </div>
  )
}
