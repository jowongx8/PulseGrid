import { LoaderCircle, Radio, RefreshCw, WifiOff } from 'lucide-react'
import type { ServiceDTO } from '@/data/contracts'
import { deriveStatusSummary } from './status-summary'
import type { ConnectionStatus } from './live-presentation'

const connections = {
  connecting: { label: 'Connecting', Icon: LoaderCircle, description: 'Connecting to live updates and loading monitoring data.' },
  live: { label: 'Live', Icon: Radio, description: 'Live updates are connected.' },
  reconnecting: { label: 'Reconnecting', Icon: RefreshCw, description: 'Restoring live updates. Displayed information may be outdated.' },
  offline: { label: 'Offline', Icon: WifiOff, description: 'Live updates are unavailable. Available information may be outdated.' },
}

function ConnectionIndicator({ status }: { status: ConnectionStatus }) {
  const { label, Icon, description } = connections[status]
  return (
    <span role="status" aria-label="Dashboard connection" aria-live="polite" aria-atomic="true" title={description}
      className={`mt-1 inline-flex items-center gap-1.5 text-[11px] ${status === 'live' ? 'text-live' : 'text-muted-foreground'}`}>
      <Icon aria-hidden="true" size={12} className={status === 'connecting' || status === 'reconnecting' ? 'motion-safe:animate-spin' : undefined} />
      <span>{label}</span><span className="sr-only"> — {description}</span>
    </span>
  )
}

interface StatusSummaryProps {
  services: readonly ServiceDTO[] | null
  connectionStatus: ConnectionStatus
  unavailable?: boolean
}

export function StatusSummary({ services, connectionStatus, unavailable = false }: StatusSummaryProps) {
  const summary = services === null ? null : deriveStatusSummary(services)
  const { total, up, down, unknown, healthyPercentage } = summary ?? { total: null, up: null, down: null, unknown: null, healthyPercentage: null }
  const headline = total === null
    ? unavailable ? 'Service statuses unavailable' : 'Loading service statuses…'
    : total === 0
    ? 'No services monitored'
    : down !== null && down > 0
      ? `${down} ${down === 1 ? 'service' : 'services'} down`
      : unknown !== null && unknown > 0
        ? `${unknown} service ${unknown === 1 ? 'status' : 'statuses'} unknown`
        : 'All monitored services operational'
  const emphasis = down !== null && down > 0 ? 'text-down' : total !== null && total > 0 && unknown === 0 ? 'text-op' : 'text-muted-foreground'
  const dot = down !== null && down > 0 ? 'bg-down' : total !== null && total > 0 && unknown === 0 ? 'bg-op' : 'bg-muted-foreground'
  const chips = [
    { label: 'Down', count: down, dot: 'bg-down' },
    { label: 'Operational', count: up, dot: 'bg-op' },
    { label: 'Unknown', count: unknown, dot: 'bg-unknown' },
  ]

  return (
    <section
      aria-label="Service status summary"
      className="flex shrink-0 flex-col justify-between gap-2 border-b bg-card/40 px-3 py-2 sm:flex-row sm:items-center sm:gap-3 sm:px-5 sm:py-3"
    >
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span aria-hidden="true" className={`size-2.5 shrink-0 rounded-full ${dot}`} />
          <h2 className={`text-sm leading-tight font-semibold ${emphasis}`}>{headline}</h2>
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          {healthyPercentage !== null && <>{healthyPercentage}% healthy · </>}
          {total === null ? unavailable ? 'Monitoring summary unavailable' : 'Loading monitoring summary…' : <>{total} {total === 1 ? 'service' : 'services'} monitored</>}
        </p>
        <ConnectionIndicator status={connectionStatus} />
      </div>
      <ul aria-label="Service counts" className="flex shrink-0 flex-wrap gap-1.5">
        {chips.map(({ label, count, dot: chipDot }) => (
          <li
            key={label}
            className="flex items-center gap-1.5 rounded-full border bg-background px-2.5 py-1 text-xs font-medium"
          >
            <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${count === null ? 'bg-muted-foreground' : chipDot}`} />
            <span aria-label={count === null ? `${label} count unavailable` : undefined} className="font-semibold tabular-nums">{count ?? '—'}</span>
            <span className="sr-only sm:not-sr-only">{label}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}
