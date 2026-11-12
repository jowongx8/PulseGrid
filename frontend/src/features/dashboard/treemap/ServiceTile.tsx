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
  const fullRows = variant === 'large'
    || (variant === 'medium' && bounds.width - 18 >= 40 && bounds.height - 16 >= 39)
  const padding = variant === 'small' ? 8 : 18
  const nameWidth = bounds.width - padding - (fullRows ? 0 : 18)
  const nameRow = variant !== 'tiny' && nameWidth >= 40 && bounds.height - (variant === 'small' ? 8 : 16) >= 15
  const nameSize = variant === 'large' ? 12 : variant === 'small' ? 11 : Math.min(12, Math.max(11, nameWidth / 8))
  const smallIdentity = bounds.width >= 44 && bounds.height >= 26
  // Reserve padding, the name/status rows and their gap before growing the logo.
  const identitySize = variant === 'large'
    ? Math.min(36, Math.max(20, Math.floor(Math.min(bounds.width / 4, bounds.height - 49))))
    : 20
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
        className="block size-full cursor-pointer rounded-[inherit] text-left focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
      >
        <div aria-hidden="true" className="h-full min-w-0">
          {fullRows ? (
            <div className="flex h-full min-w-0 flex-col justify-between gap-1 p-2 pl-2.5">
              <div className="flex items-start justify-between gap-1">
                <ServiceIdentity serviceId={service.id} name={service.name} size={identitySize} />
                <Icon size={14} className="shrink-0" />
              </div>
              <div className="min-w-0">
                <p className="truncate leading-[15px] font-medium" style={{ fontSize: nameSize }}>{service.name}</p>
                {variant === 'large' && <p className="mt-0.5 truncate text-[10px] leading-3 text-foreground/80">{label}</p>}
              </div>
            </div>
          ) : nameRow ? (
            <div className={`flex h-full min-w-0 items-center gap-1 ${variant === 'small' ? 'p-1' : 'p-2 pl-2.5'}`}>
              <p className={`min-w-0 flex-1 truncate font-medium ${variant === 'small' ? 'leading-[14px]' : 'leading-[15px]'}`} style={{ fontSize: nameSize }}>{service.name}</p>
              <Icon size={14} className="shrink-0" />
            </div>
          ) : variant !== 'tiny' ? (
            <div className={`flex h-full items-center gap-1 p-1 ${smallIdentity ? 'justify-between' : 'justify-center'}`}>
              {smallIdentity && <ServiceIdentity serviceId={service.id} name={service.name} size={18} />}
              <Icon size={14} className="shrink-0" />
            </div>
          ) : bounds.width >= 16 && bounds.height >= 16 ? (
            <span className="tiny-status absolute top-1 right-1 size-2" />
          ) : null}
        </div>
      </button>
    </li>
  )
}
