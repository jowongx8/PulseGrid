import { CircleCheck, CircleHelp, OctagonX } from 'lucide-react'
import type { ServiceDTO } from '@/data/contracts'
import type { ServicePlacement } from './layout'
import './treemap.css'

const statuses = {
  up: { label: 'Operational', Icon: CircleCheck },
  down: { label: 'Down', Icon: OctagonX },
  unknown: { label: 'Unknown', Icon: CircleHelp },
}

// Keep the identity presentation in one place for a future logo + fallback.
function ServiceIdentity({ name, size }: { name: string; size: 18 | 20 }) {
  const words = name.trim().split(/\s+/u)
  const initials = words.length > 1
    ? words.slice(0, 2).map((word) => Array.from(word)[0] ?? '').join('')
    : Array.from(words[0]).slice(0, 2).join('')

  return (
    <span
      data-service-identity="monogram"
      className="flex shrink-0 items-center justify-center overflow-hidden rounded-[5px] bg-primary text-[9px] leading-none font-semibold text-primary-foreground"
      style={{ width: size, height: size }}
    >
      {Array.from(initials.toUpperCase()).slice(0, 2).join('') || '?'}
    </span>
  )
}

export function ServiceTile({ service, placement }: { service: ServiceDTO; placement: ServicePlacement }) {
  const { allocation, bounds } = placement
  const { label, Icon } = statuses[service.status.state]
  const variant = allocation.width >= 96 && allocation.height >= 74 ? 'large'
    : allocation.width >= 58 && allocation.height >= 40 ? 'medium'
      : allocation.width < 30 || allocation.height < 30 ? 'tiny' : 'small'
  const fullRows = variant === 'large'
    || (variant === 'medium' && bounds.width - 18 >= 38 && bounds.height - 16 >= 39)
  const smallMonogram = bounds.width >= 44 && bounds.height >= 26
  const accessibleName = `${service.name}: ${label}`

  return (
    <li
      aria-label={accessibleName}
      title={accessibleName}
      data-service-id={service.id}
      data-status={service.status.state}
      data-variant={variant}
      className="service-tile absolute overflow-hidden rounded-md text-foreground"
      style={{ left: bounds.x, top: bounds.y, width: bounds.width, height: bounds.height }}
    >
      <div aria-hidden="true" className="h-full min-w-0">
        {fullRows ? (
          <div className="flex h-full min-w-0 flex-col justify-between gap-1 p-2 pl-2.5">
            <div className="flex items-start justify-between gap-1">
              <ServiceIdentity name={service.name} size={20} />
              <Icon size={14} className="shrink-0" />
            </div>
            <div className="min-w-0">
              <p className="truncate text-xs leading-tight font-medium">{service.name}</p>
              {variant === 'large' && <p className="mt-0.5 truncate text-[10px] leading-3 text-foreground/80">{label}</p>}
            </div>
          </div>
        ) : variant === 'medium' ? (
          <div className="flex h-full min-w-0 items-center gap-1 p-2 pl-2.5">
            <p className="min-w-0 flex-1 truncate text-xs leading-tight font-medium">{service.name}</p>
            <Icon size={14} className="shrink-0" />
          </div>
        ) : variant === 'small' ? (
          <div className={`flex h-full items-center gap-1 p-1 ${smallMonogram ? 'justify-between' : 'justify-center'}`}>
            {smallMonogram && <ServiceIdentity name={service.name} size={18} />}
            <Icon size={14} className="shrink-0" />
          </div>
        ) : bounds.width >= 16 && bounds.height >= 16 ? (
          <span className="tiny-status absolute top-1 right-1 size-2" />
        ) : null}
      </div>
    </li>
  )
}
