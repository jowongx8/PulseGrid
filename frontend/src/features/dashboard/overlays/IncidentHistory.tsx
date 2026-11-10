import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { compareTimestamps, formatTimestamp } from '../timestamps'

interface IncidentHistoryProps {
  incidents: IncidentsResponse
  services: readonly ServiceDTO[]
  onSelectService?: (id: string) => void
}

export function IncidentHistory({ incidents, services, onSelectService }: IncidentHistoryProps) {
  const byID = new Map(services.map((service) => [service.id, service]))
  // Stable sorting retains the backend's internal-ID tie order without inventing it.
  const sections = [
    { title: 'Active incidents', active: true, empty: 'No active incidents.', items: [...incidents.active].sort((a, b) => compareTimestamps(b.startedAt, a.startedAt)) },
    { title: 'Recently resolved', active: false, empty: 'No recently resolved incidents.', items: [...incidents.resolved].sort((a, b) => (
      a.resolvedAt === null ? b.resolvedAt === null ? 0 : 1 : b.resolvedAt === null ? -1 : compareTimestamps(b.resolvedAt, a.resolvedAt)
    )) },
  ]

  return (
    <div className="space-y-5">
      {sections.map(({ title, active, empty, items }) => (
        <section key={title} aria-label={title}>
          <h3 className="mb-2 flex items-center gap-2 text-xs font-semibold tracking-wider text-muted-foreground uppercase">
            {title}<span className="rounded-full bg-muted px-1.5 text-[10px] leading-4 tabular-nums">{items.length}</span>
          </h3>
          {items.length === 0 ? <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">{empty}</p> : (
            <ul className="space-y-2">
              {items.map((incident) => {
                const service = byID.get(incident.serviceId)
                const content = (
                  <>
                    <div className="flex items-center justify-between gap-2">
                      <span className="min-w-0 truncate text-sm font-medium">{service?.name ?? `Unavailable service (${incident.serviceId})`}</span>
                      <span className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] leading-4 ${active ? 'bg-down-surface' : 'bg-op-surface'}`}>{active ? 'Ongoing' : 'Resolved'}</span>
                    </div>
                    <p className="mt-1 text-[11px] text-muted-foreground">Started: <time dateTime={incident.startedAt}>{formatTimestamp(incident.startedAt)}</time></p>
                    {!active && <p className="mt-1 text-[11px] text-muted-foreground">{incident.resolvedAt === null ? 'Resolution time unavailable' : <>Resolved: <time dateTime={incident.resolvedAt}>{formatTimestamp(incident.resolvedAt)}</time></>}</p>}
                    {!service && <p className="mt-1 text-[11px] text-muted-foreground">Service details unavailable.</p>}
                  </>
                )
                return (
                  <li key={`${incident.serviceId}|${incident.startedAt}`} data-incident-key={`${incident.serviceId}|${incident.startedAt}`}>
                    {onSelectService && service ? (
                      <button type="button" onClick={() => onSelectService(service.id)} className="block w-full rounded-md border bg-background p-2.5 text-left hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">{content}</button>
                    ) : <div className="rounded-md border bg-background p-2.5">{content}</div>}
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      ))}
    </div>
  )
}
