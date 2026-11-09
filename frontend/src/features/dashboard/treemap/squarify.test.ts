import { describe, expect, it } from 'vitest'
import { squarify } from './squarify'
import type { Rect } from './squarify'

const bounds = { x: 10, y: 20, width: 800, height: 500 }
const weighted = (weights: number[]) => weights.map((weight, i) => ({ id: `item-${i}`, weight }))

function verifyPartition(rectangles: Rect[], parent: Rect) {
  const tolerance = Math.max(1, parent.width, parent.height) * 1e-8
  let area = 0
  let overlap = 0
  for (const rect of rectangles) {
    expect(Object.values(rect).every(Number.isFinite)).toBe(true)
    expect(rect.width).toBeGreaterThanOrEqual(0)
    expect(rect.height).toBeGreaterThanOrEqual(0)
    expect(rect.x).toBeGreaterThanOrEqual(parent.x - tolerance)
    expect(rect.y).toBeGreaterThanOrEqual(parent.y - tolerance)
    expect(rect.x + rect.width).toBeLessThanOrEqual(parent.x + parent.width + tolerance)
    expect(rect.y + rect.height).toBeLessThanOrEqual(parent.y + parent.height + tolerance)
    area += rect.width * rect.height
  }
  for (let i = 0; i < rectangles.length; i += 1) {
    for (let j = i + 1; j < rectangles.length; j += 1) {
      const a = rectangles[i]
      const b = rectangles[j]
      const width = Math.max(0, Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x))
      const height = Math.max(0, Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y))
      overlap = Math.max(overlap, width * height)
    }
  }
  expect(overlap).toBeLessThanOrEqual(tolerance * tolerance)
  expect(Math.abs(area - parent.width * parent.height)).toBeLessThanOrEqual(Math.max(1, area) * 1e-8)
}

describe('squarify', () => {
  it.each([
    [1], [1, 1, 1, 1], [6, 6, 4, 3, 2, 2, 1], [1, 1000, 2, 1],
    [Number.MAX_VALUE, Number.MAX_VALUE / 2],
  ])('allocates proportional areas for weights %j', (...weights) => {
    const items = weighted(weights)
    const result = squarify(items, bounds)
    const max = Math.max(...weights)
    const total = weights.reduce((sum, weight) => sum + weight / max, 0)
    expect(result.map((item) => item.id)).toEqual(items.map((item) => item.id))
    result.forEach(({ rect }, i) => {
      expect(rect.width * rect.height / (bounds.width * bounds.height)).toBeCloseTo((weights[i] / max) / total, 10)
    })
    verifyPartition(result.map((item) => item.rect), bounds)
  })

  it('is deterministic and does not mutate ordered inputs or bounds', () => {
    const items = Object.freeze(weighted([2, 1, 9, 3]).map((item) => Object.freeze(item)))
    const rectangle = Object.freeze({ ...bounds })
    expect(squarify(items, rectangle)).toEqual(squarify(items, rectangle))
    expect(items.map((item) => item.weight)).toEqual([2, 1, 9, 3])
  })

  it('produces square-like rectangles for the paper’s representative example', () => {
    const result = squarify(weighted([6, 6, 4, 3, 2, 2, 1]), { x: 0, y: 0, width: 6, height: 4 })
    expect(Math.max(...result.map(({ rect }) => Math.max(rect.width / rect.height, rect.height / rect.width)))).toBeLessThan(3)
  })

  it.each([
    { x: 0, y: 0, width: 10000, height: 10 },
    { x: 0, y: 0, width: 10, height: 10000 },
    { x: -5.25, y: 2.125, width: 33.333, height: 14.781 },
    { x: 0, y: 0, width: 0.0001, height: 0.0003 },
  ])('stays contained and non-overlapping in $width × $height', (rectangle) => {
    verifyPartition(squarify(weighted([8, 7, 5, 4, 3, 1]), rectangle).map((item) => item.rect), rectangle)
  })

  it.each([100, 500, 1000])('partitions a growing collection of %i items', (count) => {
    const items = weighted(Array.from({ length: count }, (_, i) => 1 + (i * 7) % 10))
    const result = squarify(items, bounds)
    expect(result).toHaveLength(count)
    verifyPartition(result.map((item) => item.rect), bounds)
  })

  it('handles empty, all-invalid and duplicate-ID inputs', () => {
    expect(squarify([], bounds)).toEqual([])
    expect(squarify(weighted([0, -1, NaN, Infinity, -Infinity]), bounds)).toEqual([])
    expect(squarify([{ id: 'same', weight: 1 }, { id: 'same', weight: 2 }], bounds)).toEqual([])
  })

  it('excludes invalid weights without assigning them artificial area', () => {
    const result = squarify(weighted([NaN, 0, 2, Infinity, -1]), bounds)
    expect(result).toEqual([{ id: 'item-2', rect: bounds }])
  })

  it.each([
    { ...bounds, width: 0 }, { ...bounds, height: 0 }, { ...bounds, width: -1 },
    { ...bounds, height: NaN }, { ...bounds, width: Infinity }, { ...bounds, x: Infinity },
    { ...bounds, y: NaN }, { ...bounds, width: Number.MAX_VALUE, height: Number.MAX_VALUE },
    { ...bounds, x: Number.MAX_VALUE, width: Number.MAX_VALUE, height: 0.1 },
  ])('rejects invalid or degenerate bounds %j', (rectangle) => {
    expect(squarify(weighted([1, 2]), rectangle)).toEqual([])
  })

  it('does not emit invalid geometry when tiny weights underflow', () => {
    const result = squarify(weighted([Number.MIN_VALUE, Number.MAX_VALUE]), bounds)
    expect(result).toEqual([{ id: 'item-1', rect: bounds }])
  })
})
