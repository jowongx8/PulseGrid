import { useState } from 'react'
import { DashboardShell } from '@/features/dashboard/DashboardShell'
import { deriveConnectionStatus, hasResourceFailure } from '@/features/dashboard/live-presentation'
import { useLiveStatus } from '@/data/live/useLiveStatus'

function App() {
  const snapshot = useLiveStatus()
  const [hasBeenLive, setHasBeenLive] = useState(false)
  // This latch affects labels only; synchronization remains owned by FE-012.
  if (!hasBeenLive && snapshot.transport === 'open' && snapshot.synchronization === 'synchronized') setHasBeenLive(true)
  const [servicesFailed, setServicesFailed] = useState(false)
  const [incidentsFailed, setIncidentsFailed] = useState(false)
  // FE-012 retains the latest issue batch, not a per-resource error history.
  // Keep initial error presentation stable until that resource is accepted.
  const servicesUnavailable = snapshot.services === null && (servicesFailed || hasResourceFailure(snapshot, 'status'))
  const incidentsUnavailable = snapshot.incidents === null && (incidentsFailed || hasResourceFailure(snapshot, 'incidents'))
  if (servicesFailed !== servicesUnavailable) setServicesFailed(servicesUnavailable)
  if (incidentsFailed !== incidentsUnavailable) setIncidentsFailed(incidentsUnavailable)

  return (
    <DashboardShell
      services={snapshot.services}
      incidents={snapshot.incidents}
      connectionStatus={deriveConnectionStatus(snapshot, hasBeenLive, servicesUnavailable || incidentsUnavailable)}
      servicesUnavailable={servicesUnavailable}
      incidentsUnavailable={incidentsUnavailable}
    />
  )
}

export default App
