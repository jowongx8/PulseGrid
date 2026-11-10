import type { IncidentsResponse, ServiceDTO } from '@/data/contracts'
import { IncidentHistory } from './IncidentHistory'

export function IncidentsDrawer({ incidents, services, onSelectService }: { incidents: IncidentsResponse; services: readonly ServiceDTO[]; onSelectService: (id: string) => void }) {
  return <div className="p-4"><IncidentHistory incidents={incidents} services={services} onSelectService={onSelectService} /></div>
}
