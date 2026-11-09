import type { IncidentDTO, ServiceDTO } from '@/data/contracts'
import { Navbar } from './Navbar'
import { StatusSummary } from './StatusSummary'

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
        <div className="absolute inset-0 flex items-center justify-center p-2">
          <p className="text-center text-xs text-muted-foreground">
            Service map coming soon.
          </p>
        </div>
      </main>
    </div>
  )
}
