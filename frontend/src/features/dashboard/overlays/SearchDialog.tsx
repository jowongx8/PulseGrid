import { Search } from 'lucide-react'
import { useId, useLayoutEffect, useState } from 'react'
import type { ServiceDTO } from '@/data/contracts'
import { getCategoryLabel } from '../category-presentation'
import { ServiceIdentity } from '../ServiceIdentity'
import { ServiceStatus } from '../ServiceStatus'

const names = new Intl.Collator('en', { sensitivity: 'base', numeric: true })

export function SearchDialog({ services, onSelect }: { services: readonly ServiceDTO[]; onSelect: (id: string) => void }) {
  const [query, setQuery] = useState('')
  const [activeID, setActiveID] = useState<string | null>(null)
  const inputID = useId()
  const listID = useId()
  const normalized = query.trim().toLowerCase()
  const matches = normalized ? services.filter((service) => (
    service.name.toLowerCase().includes(normalized) || getCategoryLabel(service.category).toLowerCase().includes(normalized)
  )).sort((a, b) => names.compare(a.name, b.name) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)) : []
  const visible = matches.slice(0, 8)
  const active = visible.find((service) => service.id === activeID) ?? visible[0]
  const activeServiceID = active?.id
  const optionID = (id: string) => `${listID}-${id}`

  useLayoutEffect(() => {
    if (!activeServiceID) return
    const option = document.getElementById(`${listID}-${activeServiceID}`)
    option?.scrollIntoView?.({ block: 'nearest' })
  }, [activeServiceID, listID])

  return (
    <div className="flex min-h-0 flex-col">
      <div className="relative shrink-0">
        <Search size={16} aria-hidden="true" className="pointer-events-none absolute top-4 left-4 text-muted-foreground" />
        <label htmlFor={inputID} className="sr-only">Search services</label>
        <input
          id={inputID}
          data-initial-focus
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={visible.length > 0}
          aria-controls={listID}
          aria-activedescendant={active ? optionID(active.id) : undefined}
          value={query}
          onChange={(event) => { setQuery(event.target.value); setActiveID(null) }}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing || visible.length === 0) return
            const index = visible.findIndex((service) => service.id === active?.id)
            if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault()
              const next = (index + (event.key === 'ArrowDown' ? 1 : -1) + visible.length) % visible.length
              setActiveID(visible[next].id)
            } else if (event.key === 'Enter' && active) {
              event.preventDefault()
              onSelect(active.id)
            }
          }}
          placeholder="Search services or categories…"
          className="h-12 w-full bg-transparent pr-12 pl-10 text-base focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring md:text-sm"
        />
      </div>
      <ul id={listID} role="listbox" aria-label="Matching services" className="min-h-0 max-h-80 overflow-y-auto overscroll-contain border-t">
        {visible.map((service) => (
          <li
            key={service.id}
            id={optionID(service.id)}
            role="option"
            aria-selected={active?.id === service.id}
            onPointerMove={() => setActiveID(service.id)}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => onSelect(service.id)}
            className={`flex cursor-pointer items-center gap-2.5 px-4 py-2.5 ${active?.id === service.id ? 'bg-muted' : ''}`}
          >
            <ServiceIdentity serviceId={service.id} name={service.name} size={20} />
            <div className="min-w-0 flex-1">
              <p className="truncate text-xs font-medium">{service.name}</p>
              <p className="truncate text-[10px] leading-4 text-muted-foreground">{getCategoryLabel(service.category)}</p>
            </div>
            <ServiceStatus state={service.status.state} />
          </li>
        ))}
      </ul>
      <p role="status" className="shrink-0 border-t px-4 py-2 text-[11px] text-muted-foreground">
        {!normalized ? 'Search by service name or category.' : matches.length === 0 ? 'No services match your search.'
          : matches.length > 8 ? `Showing 8 of ${matches.length} matches. Refine your search.`
            : `${matches.length} ${matches.length === 1 ? 'service' : 'services'} found.`}
      </p>
    </div>
  )
}
