import type { ServiceDTO } from '@/data/contracts'
import { deriveStatusSummary } from './status-summary'
import { formatTimestamp } from './timestamps'

export function StatusSummary({ services }: { services: readonly ServiceDTO[] }) {
  const { total, up, down, unknown, healthyPercentage, latestCheckedAt } = deriveStatusSummary(services)
  const headline = total === 0
    ? 'No services monitored'
    : down > 0
      ? `${down} ${down === 1 ? 'service' : 'services'} down`
      : unknown > 0
        ? `${unknown} service ${unknown === 1 ? 'status' : 'statuses'} unknown`
        : 'All monitored services operational'
  const emphasis = down > 0 ? 'text-down' : total > 0 && unknown === 0 ? 'text-op' : 'text-muted-foreground'
  const dot = down > 0 ? 'bg-down' : total > 0 && unknown === 0 ? 'bg-op' : 'bg-muted-foreground'
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
          {total} {total === 1 ? 'service' : 'services'} monitored
        </p>
        <p className="mt-1 text-[11px] text-muted-foreground">
          {latestCheckedAt === null ? 'No service checks recorded' : (
            <>
              Latest service check:{' '}
              <time dateTime={latestCheckedAt}>{formatTimestamp(latestCheckedAt)}</time>
            </>
          )}
        </p>
      </div>
      <ul aria-label="Service counts" className="flex shrink-0 flex-wrap gap-1.5">
        {chips.map(({ label, count, dot: chipDot }) => (
          <li
            key={label}
            className="flex items-center gap-1.5 rounded-full border bg-background px-2.5 py-1 text-xs font-medium"
          >
            <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${chipDot}`} />
            <span className="font-semibold tabular-nums">{count}</span>
            <span className="sr-only sm:not-sr-only">{label}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}
