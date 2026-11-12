import { ExternalLink } from 'lucide-react'
import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { getCategoryLabel } from '../category-presentation'
import { ServiceIdentity } from '../ServiceIdentity'
import { ServiceStatus } from '../ServiceStatus'
import { IncidentHistory } from './IncidentHistory'

export function ServiceDetails({ service, incidents, incidentsUnavailable = false }: { service: ServiceDTO | undefined; incidents: IncidentsResponse | null; incidentsUnavailable?: boolean }) {
  if (!service) return <p role="status" className="p-4 text-xs text-muted-foreground">This service is no longer available.</p>
  const history = incidents === null ? null : {
    active: incidents.active.filter((incident) => incident.serviceId === service.id),
    resolved: incidents.resolved.filter((incident) => incident.serviceId === service.id),
  }

  return (
    <div className="space-y-5 p-4">
      <div className="flex items-center gap-3">
        <ServiceIdentity name={service.name} size={44} />
        <div className="min-w-0 space-y-1">
          <ServiceStatus state={service.status.state} />
          <p className="break-words text-xs text-muted-foreground">{getCategoryLabel(service.category)}</p>
        </div>
      </div>
      <dl className="space-y-3 text-xs">
        <div><dt className="text-muted-foreground">Official website</dt><dd className="mt-1">{service.websiteUrl ? <a href={service.websiteUrl} target="_blank" rel="noopener noreferrer" className="inline-flex max-w-full items-center gap-1 rounded-sm underline underline-offset-2 focus-visible:outline-2 focus-visible:outline-ring"><span className="break-all">{service.websiteUrl}</span><ExternalLink size={12} className="shrink-0" aria-hidden="true" /></a> : 'Website unavailable'}</dd></div>
      </dl>
      {history !== null ? <IncidentHistory incidents={history} services={[service]} /> : (
        <section role="status" aria-label="Incident information" className="space-y-2 text-xs text-muted-foreground">
          {incidentsUnavailable ? <><h3 className="font-semibold">Incident information unavailable</h3><p>Service status is still available. Incident details couldn't be loaded.</p></>
            : <p>Loading incident information…</p>}
        </section>
      )}
    </div>
  )
}
