import type { ServiceDTO } from '@/data/contracts'
import type { ServicePlacement } from './layout'
import './treemap.css'

import { ServiceIdentity } from '../ServiceIdentity'
import { servicePresentation } from '../service-presentation'

export function ServiceTile({ service, placement, onSelect }: { service: ServiceDTO; placement: ServicePlacement; onSelect: (id: string, opener: HTMLButtonElement) => void }) {
  const { allocation, bounds } = placement
  const { label, Icon } = servicePresentation[service.status.state]
  const variant = allocation.width >= 96 && allocation.height >= 74 ? 'large'
    : allocation.width >= 58 && allocation.height >= 40 ? 'medium'
      : allocation.width < 30 || allocation.height < 30 ? 'tiny' : 'small'
  const padding = variant === 'large' ? 8 : 4
  const contentWidth = Math.max(0, bounds.width - padding * 2)
  const contentHeight = Math.max(0, bounds.height - padding * 2)
  const showName = contentWidth >= Math.min(36, Math.max(14, Array.from(service.name).length * 7)) && contentHeight >= 15
  const showCaption = variant === 'large' && contentWidth >= 76 && contentHeight >= 77
  // Reserve the name, optional status row and gaps before sizing the shared identity.
  const identitySize = Math.floor(Math.min(64, contentWidth * 0.45, contentHeight - 19 - (showCaption ? 18 : 0)))
  const showIdentity = showName && identitySize >= 20
  const nameSize = variant === 'large' ? 12 : 11
  const accessibleName = `${service.name}: ${label}`

  return (
    <li
      data-service-id={service.id}
      data-status={service.status.state}
      data-variant={variant}
      className="service-tile absolute overflow-hidden rounded-md text-foreground"
      style={{ left: bounds.x, top: bounds.y, width: bounds.width, height: bounds.height }}
    >
      <button
        type="button"
        aria-label={accessibleName}
        title={accessibleName}
        onClick={(event) => onSelect(service.id, event.currentTarget)}
        className="block size-full cursor-pointer rounded-[inherit] text-center focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
      >
        <div aria-hidden="true" className="flex h-full min-w-0 flex-col items-center justify-center gap-1" style={{ padding }}>
          {showIdentity && !showCaption && <Icon size={12} className="absolute top-1 right-1" />}
          {showIdentity && <ServiceIdentity serviceId={service.id} name={service.name} size={identitySize} />}
          {showName && <p className="w-full min-w-0 truncate leading-[15px] font-medium" style={{ fontSize: nameSize }}>{service.name}</p>}
          {showCaption ? (
            <p className="flex shrink-0 items-center justify-center gap-1 text-[10px] leading-[14px] text-foreground/80">
              <Icon size={12} className="shrink-0" />{label}
            </p>
          ) : !showIdentity && contentHeight >= (showName ? 33 : 14) && contentWidth >= 14 ? (
            <Icon size={14} className="shrink-0" />
          ) : !showName && bounds.width >= 16 && bounds.height >= 16 ? (
            <span className="tiny-status size-2 shrink-0" />
          ) : null}
        </div>
      </button>
    </li>
  )
}
