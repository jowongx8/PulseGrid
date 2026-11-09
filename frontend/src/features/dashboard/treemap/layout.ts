import type { ServiceCategory, ServiceDTO } from '@/data/contracts'
import { squarify } from './squarify'
import type { Rect } from './squarify'

// This presentation order takes precedence over sorting categories by weight.
export const categoryLabels = {
  'developer-cloud': 'Developer & Cloud',
  ai: 'AI',
  communication: 'Communication',
  'social-media': 'Social Media',
  productivity: 'Productivity',
  entertainment: 'Entertainment',
  gaming: 'Gaming',
  commerce: 'Commerce',
} satisfies Record<ServiceCategory, string>

export type GeometryService = Pick<ServiceDTO, 'id' | 'category' | 'weight'>

export interface ServicePlacement {
  id: string
  allocation: Rect
  bounds: Rect
}

export interface CategoryPlacement {
  category: ServiceCategory
  bounds: Rect
  label: Rect | null
  content: Rect
  services: ServicePlacement[]
}

function compareIDs(a: GeometryService, b: GeometryService): number {
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
}

export function createGeometryKey(services: readonly GeometryService[]): string {
  return JSON.stringify(services.map(({ id, category, weight }) => ({ id, category, weight })).sort(compareIDs))
}

function inset(rect: Rect, gap: number): Rect {
  const horizontal = Math.min(gap, rect.width / 4)
  const vertical = Math.min(gap, rect.height / 4)
  return {
    x: rect.x + horizontal,
    y: rect.y + vertical,
    width: rect.width - horizontal * 2,
    height: rect.height - vertical * 2,
  }
}

export function createTreemapLayout(services: readonly GeometryService[], bounds: Rect): CategoryPlacement[] {
  if (new Set(services.map((service) => service.id)).size !== services.length) return []
  const valid = services.filter((service) => Number.isFinite(service.weight) && service.weight > 0)
  const largest = valid.reduce((max, service) => Math.max(max, service.weight), 0)
  if (largest === 0) return []

  const groups = new Map<ServiceCategory, GeometryService[]>()
  for (const service of valid) {
    const group = groups.get(service.category)
    if (group) group.push(service)
    else groups.set(service.category, [service])
  }
  const categories = (Object.keys(categoryLabels) as ServiceCategory[])
    .filter((category) => groups.has(category))
    .map((category) => ({
      id: category,
      weight: groups.get(category)!.reduce((sum, service) => sum + service.weight / largest, 0),
    }))

  return squarify(categories, bounds).map(({ id, rect }) => {
    const category = id as ServiceCategory
    const inner = inset({ x: 0, y: 0, width: rect.width, height: rect.height }, 3)
    const label = inner.width > 90 && inner.height > 60 ? { ...inner, height: 18 } : null
    const content = { ...inner, y: inner.y + (label?.height ?? 0), height: inner.height - (label?.height ?? 0) }
    const ordered = [...groups.get(category)!].sort((a, b) => b.weight - a.weight || compareIDs(a, b))

    return {
      category,
      bounds: rect,
      label,
      content,
      // Child coordinates are category-local; the category is positioned in the host.
      services: squarify(ordered, content).map(({ id: serviceId, rect: allocation }) => ({
        id: serviceId,
        allocation,
        bounds: inset(allocation, 1.5),
      })),
    }
  })
}
