import type { IncidentDTO, ServiceDTO } from '@/data/contracts'
import { Navbar } from './Navbar'
import { StatusSummary } from './StatusSummary'
import { ServiceTreemap } from './treemap/ServiceTreemap'

interface DashboardShellProps {
  services: readonly ServiceDTO[]
  activeIncidents: readonly IncidentDTO[]
}

export function DashboardShell({ services, activeIncidents }: DashboardShellProps) {
  return (
    <div className="flex h-dvh min-w-0 flex-col overflow-hidden bg-background">
      <Navbar activeIncidentCount={activeIncidents.length} />
      <StatusSummary services={services} />
      <main
        aria-label="Service map"
        className="relative min-h-0 min-w-0 flex-1 overflow-hidden"
      >
        <ServiceTreemap services={services} />
      </main>
    </div>
  )
}
