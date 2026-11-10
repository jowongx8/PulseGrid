import { describe, expect, it } from 'vitest'
import type { ServiceCategory } from '@/data/contracts'
import { makeService } from '@/test/services'
import { createGeometryKey, createTreemapLayout } from './layout'
import type { GeometryService } from './layout'
import type { Rect } from './squarify'

const bounds = { x: 0, y: 0, width: 1000, height: 600 }
const order: ServiceCategory[] = [
  'developer-cloud', 'ai', 'communication', 'social-media',
  'productivity', 'entertainment', 'gaming', 'commerce',
]
const service = (id: string, category: ServiceCategory, weight: number): GeometryService => ({ id, category, weight })

function expectInside(child: Rect, parent: Rect) {
  const tolerance = 1e-7
  expect(Object.values(child).every(Number.isFinite)).toBe(true)
  expect(child.width).toBeGreaterThanOrEqual(0)
  expect(child.height).toBeGreaterThanOrEqual(0)
  expect(child.x).toBeGreaterThanOrEqual(parent.x - tolerance)
  expect(child.y).toBeGreaterThanOrEqual(parent.y - tolerance)
  expect(child.x + child.width).toBeLessThanOrEqual(parent.x + parent.width + tolerance)
  expect(child.y + child.height).toBeLessThanOrEqual(parent.y + parent.height + tolerance)
}

describe('createTreemapLayout', () => {
  it('aggregates category weights, omits empty categories and sorts services deterministically', () => {
    const services = [service('z', 'ai', 2), service('b', 'developer-cloud', 1), service('a', 'developer-cloud', 1), service('heavy', 'developer-cloud', 6)]
    const result = createTreemapLayout(services, bounds)
    expect(result.map((group) => group.category)).toEqual(['developer-cloud', 'ai'])
    expect(result[0].services.map((item) => item.id)).toEqual(['heavy', 'a', 'b'])
    expect(result[0].bounds.width * result[0].bounds.height / 600000).toBeCloseTo(0.8, 10)
    expect(result[1].bounds.width * result[1].bounds.height / 600000).toBeCloseTo(0.2, 10)
    expect(createTreemapLayout([...services].reverse(), bounds)).toEqual(result)
    const body = result[0].content
    for (const placement of result[0].services) {
      const expectedWeight = placement.id === 'heavy' ? 6 : 1
      expect(placement.allocation.width * placement.allocation.height / (body.width * body.height)).toBeCloseTo(expectedWeight / 8, 10)
    }
  })

  it('preserves presentation order with a much heavier final category', () => {
    const services = [
      ...order.filter((category) => category !== 'commerce').map((category) => service(category, category, 1)),
      ...Array.from({ length: 100 }, (_, i) => service(`shop-${i}`, 'commerce', 10)),
    ].reverse()
    const layout = createTreemapLayout(services, bounds)
    expect(layout.map((group) => group.category)).toEqual(order)
    expect(layout.flatMap((group) => group.services)).toHaveLength(107)
    for (const group of layout) {
      expectInside(group.bounds, bounds)
      const fraction = group.bounds.width * group.bounds.height / (bounds.width * bounds.height)
      expect(fraction).toBeCloseTo((group.category === 'commerce' ? 1000 : 1) / 1007, 10)
      for (const placement of group.services) {
        expectInside(placement.allocation, group.content)
        expectInside(placement.bounds, placement.allocation)
      }
    }
    for (let i = 0; i < layout.length; i += 1) {
      for (let j = i + 1; j < layout.length; j += 1) {
        const a = layout[i].bounds
        const b = layout[j].bounds
        const overlap = Math.max(0, Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x))
          * Math.max(0, Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y))
        expect(overlap).toBeLessThan(1e-7)
      }
    }
  })

  it('keeps allocation separate from headers and visual insets', () => {
    const [group] = createTreemapLayout([service('only', 'ai', 1)], { x: 0, y: 0, width: 200, height: 100 })
    expect(group.label).toEqual({ x: 3, y: 3, width: 194, height: 18 })
    expect(group.content).toEqual({ x: 3, y: 21, width: 194, height: 76 })
    expect(group.services[0].allocation).toEqual(group.content)
    expect(group.services[0].bounds).toEqual({ x: 4.5, y: 22.5, width: 191, height: 73 })
  })

  it.each([
    [96, 100, false], [97, 66, false], [97, 67, true], [2, 3, false], [0.01, 0.02, false],
  ])('handles header and inset thresholds at %s × %s', (width, height, hasLabel) => {
    const rectangle = { x: 0, y: 0, width, height }
    const [group] = createTreemapLayout([service('only', 'ai', 1)], rectangle)
    expect(group.label !== null).toBe(hasLabel)
    expectInside(group.content, rectangle)
    expectInside(group.services[0].bounds, group.content)
    expect(group.services[0].bounds.width).toBeGreaterThan(0)
    expect(group.services[0].bounds.height).toBeGreaterThan(0)
  })

  it('rejects duplicate identities and handles empty/invalid weights or containers', () => {
    expect(createTreemapLayout([], bounds)).toEqual([])
    expect(createTreemapLayout([service('x', 'ai', NaN)], bounds)).toEqual([])
    expect(createTreemapLayout([service('x', 'ai', 1), service('x', 'gaming', 2)], bounds)).toEqual([])
    expect(createTreemapLayout([service('x', 'ai', 1)], { ...bounds, height: 0 })).toEqual([])
  })

  it.each([100, 500, 1000])('contains every service in a %i-service catalogue', (count) => {
    const services = Array.from({ length: count }, (_, i) => service(`service-${i}`, order[i % order.length], 1 + i % 10))
    const layout = createTreemapLayout(services, bounds)
    expect(layout.flatMap((group) => group.services)).toHaveLength(count)
    for (const group of layout) {
      expectInside(group.bounds, bounds)
      expectInside(group.content, { x: 0, y: 0, width: group.bounds.width, height: group.bounds.height })
      for (const item of group.services) {
        expectInside(item.allocation, group.content)
        expectInside(item.bounds, item.allocation)
      }
    }
  })

  it.each([[304, 160], [304, 360], [359, 400], [414, 450], [624, 300]])('keeps dense mobile geometry bounded at %s × %s', (width, height) => {
    const rectangle = { x: 0, y: 0, width, height }
    const services = Array.from({ length: 500 }, (_, i) => service(`service-${i}`, order[i % order.length], 1 + i % 10))
    // ServiceTreemap canonicalizes geometry inputs before passing them to the adapter.
    const layout = createTreemapLayout(JSON.parse(createGeometryKey(services)) as GeometryService[], rectangle)
    expect(layout.flatMap((group) => group.services)).toHaveLength(500)
    expect(layout.map((group) => group.category)).toEqual(order)
    expect(createTreemapLayout(JSON.parse(createGeometryKey([...services].reverse())) as GeometryService[], rectangle)).toEqual(layout)
    for (const group of layout) {
      expectInside(group.bounds, rectangle)
      if (group.label) expectInside(group.label, { x: 0, y: 0, width: group.bounds.width, height: group.bounds.height })
      for (const item of group.services) {
        expectInside(item.allocation, group.content)
        expectInside(item.bounds, item.allocation)
      }
    }
  })
})

describe('geometry inputs', () => {
  it('excludes presentation fields and input order without mutating DTOs', () => {
    const services = [makeService('b', 'up'), makeService('a', 'down')]
    const before = structuredClone(services)
    const changed = services.map((item) => ({
      ...item, name: 'Renamed', websiteUrl: 'https://changed.example',
      status: { state: 'unknown' as const, lastCheckedAt: '2026-11-10T12:00:00.123456789Z' },
    })).reverse()
    expect(createGeometryKey(changed)).toBe(createGeometryKey(services))
    expect(createTreemapLayout(changed, bounds)).toEqual(createTreemapLayout(services, bounds))
    expect(services).toEqual(before)
  })

  it('changes the geometry key for membership, weight and category changes', () => {
    const original = [service('x', 'ai', 1)]
    for (const changed of [[], [service('y', 'ai', 1)], [service('x', 'ai', 2)], [service('x', 'gaming', 1)]]) {
      expect(createGeometryKey(changed)).not.toBe(createGeometryKey(original))
    }
  })
})
