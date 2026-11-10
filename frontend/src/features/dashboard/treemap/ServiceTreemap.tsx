import { useId, useMemo } from 'react'
import type { ServiceDTO } from '@/data/contracts'
import { createGeometryKey, createTreemapLayout } from './layout'
import { categoryLabels, isKnownCategory } from '../category-presentation'
import type { GeometryService } from './layout'
import { ServiceTile } from './ServiceTile'
import { useContainerSize } from './useContainerSize'

export function ServiceTreemap({ services, onSelectService }: { services: readonly ServiceDTO[]; onSelectService: (id: string, opener: HTMLButtonElement) => void }) {
  const headingPrefix = useId()
  const { ref, size: { width, height } } = useContainerSize()
  const geometryKey = useMemo(() => createGeometryKey(services), [services])
  const layout = useMemo(() => createTreemapLayout(
    // The key is produced locally from the geometry-only projection, not API JSON.
    JSON.parse(geometryKey) as GeometryService[],
    { x: 0, y: 0, width, height },
  ), [geometryKey, width, height])
  const byID = new Map(services.map((service) => [service.id, service]))
  const unsupported = services.some((service) => !isKnownCategory(service.category))
  const hasTiles = layout.some((category) => category.services.length > 0)

  return (
    <>
      <div className="absolute inset-0 hidden p-2 md:block">
        <div ref={ref} role="group" aria-label="Service treemap" className="relative size-full overflow-hidden">
          {width > 0 && height > 0 && !hasTiles && (
            <p className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-muted-foreground">
              {unsupported ? 'Service map unavailable: unsupported categories. Use search to view services.' : services.length === 0 ? 'No services to display' : 'Service map unavailable for this data'}
            </p>
          )}
          {layout.map(({ category, bounds, label, services: placements }) => (
            <section
              key={category}
              data-category={category}
              aria-labelledby={`${headingPrefix}-${category}`}
              className="absolute overflow-hidden"
              style={{ left: bounds.x, top: bounds.y, width: bounds.width, height: bounds.height }}
            >
              <h2
                id={`${headingPrefix}-${category}`}
                className={label
                  ? 'absolute truncate px-1 text-[10px] leading-[18px] font-semibold tracking-wider text-muted-foreground/80 uppercase'
                  : 'sr-only'}
                style={label ? { left: label.x, top: label.y, width: label.width, height: label.height } : undefined}
              >
                {categoryLabels[category]}
              </h2>
              <ul className="absolute inset-0" aria-label={`${categoryLabels[category]} services`}>
                {placements.map((placement) => {
                  const service = byID.get(placement.id)
                  return service ? <ServiceTile key={service.id} service={service} placement={placement} onSelect={onSelectService} /> : null
                })}
              </ul>
            </section>
          ))}
        </div>
      </div>
      <div className="absolute inset-0 flex items-center justify-center p-4 md:hidden">
        <p className="text-center text-xs text-muted-foreground">
          The service map is available on wider screens.
        </p>
      </div>
    </>
  )
}
