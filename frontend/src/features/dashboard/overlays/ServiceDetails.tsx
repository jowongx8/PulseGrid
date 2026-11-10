import { ExternalLink } from 'lucide-react'
import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { getCategoryLabel } from '../category-presentation'
import { ServiceIdentity } from '../ServiceIdentity'
import { ServiceStatus } from '../ServiceStatus'
import { formatTimestamp } from '../timestamps'
import { IncidentHistory } from './IncidentHistory'

export function ServiceDetails({ service, incidents }: { service: ServiceDTO | undefined; incidents: IncidentsResponse }) {
  if (!service) return <p role="status" className="p-4 text-xs text-muted-foreground">This service is no longer available.</p>
  const history = {
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
        <div><dt className="text-muted-foreground">Last checked</dt><dd className="mt-1">{service.status.lastCheckedAt === null ? 'No service checks recorded' : <time dateTime={service.status.lastCheckedAt}>{formatTimestamp(service.status.lastCheckedAt)}</time>}</dd></div>
        <div><dt className="text-muted-foreground">Official website</dt><dd className="mt-1">{service.websiteUrl ? <a href={service.websiteUrl} target="_blank" rel="noopener noreferrer" className="inline-flex max-w-full items-center gap-1 rounded-sm underline underline-offset-2 focus-visible:outline-2 focus-visible:outline-ring"><span className="break-all">{service.websiteUrl}</span><ExternalLink size={12} className="shrink-0" aria-hidden="true" /></a> : 'Website unavailable'}</dd></div>
      </dl>
      <IncidentHistory incidents={history} services={[service]} />
    </div>
  )
}
