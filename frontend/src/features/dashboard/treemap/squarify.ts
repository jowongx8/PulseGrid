export interface Rect {
  x: number
  y: number
  width: number
  height: number
}

export interface WeightedItem {
  id: string
  weight: number
}

export interface Placement {
  id: string
  rect: Rect
}

function worstAspect(sum: number, minimum: number, maximum: number, side: number): number {
  const thickness = sum / side
  if (!(thickness > 0) || !Number.isFinite(thickness)) return Infinity
  return Math.max(thickness / (minimum / thickness), (maximum / thickness) / thickness)
}

// Original implementation of the row heuristic described by Bruls, Huizing and
// van Wijk: https://vanwijk.win.tue.nl/stm.pdf. Caller order is intentional.
export function squarify(items: readonly WeightedItem[], bounds: Rect): Placement[] {
  const { x, y, width, height } = bounds
  const area = width * height
  if (![x, y, width, height, x + width, y + height, area].every(Number.isFinite)
    || width <= 0 || height <= 0 || area <= 0) return []
  if (new Set(items.map((item) => item.id)).size !== items.length) return []

  const valid = items.filter((item) => Number.isFinite(item.weight) && item.weight > 0)
  const largest = valid.reduce((max, item) => Math.max(max, item.weight), 0)
  if (largest === 0) return []

  // Scaling before summation also supports finite weights whose raw sum overflows.
  const scaled = valid.map((item) => ({ id: item.id, weight: item.weight / largest }))
  const total = scaled.reduce((sum, item) => sum + item.weight, 0)
  const areas = scaled.map((item) => ({ id: item.id, area: (item.weight / total) * area }))
    .filter((item) => item.area > 0)
  const result: Placement[] = []
  let remaining = { ...bounds }
  let start = 0

  while (start < areas.length && remaining.width > 0 && remaining.height > 0) {
    const vertical = remaining.width >= remaining.height
    const side = vertical ? remaining.height : remaining.width
    let end = start + 1
    let sum = areas[start].area
    let minimum = sum
    let maximum = sum
    let worst = worstAspect(sum, minimum, maximum, side)

    while (end < areas.length) {
      const next = areas[end].area
      const nextSum = sum + next
      const nextMin = Math.min(minimum, next)
      const nextMax = Math.max(maximum, next)
      const nextWorst = worstAspect(nextSum, nextMin, nextMax, side)
      if (nextWorst > worst) break
      sum = nextSum
      minimum = nextMin
      maximum = nextMax
      worst = nextWorst
      end += 1
    }

    const length = vertical ? remaining.width : remaining.height
    const thickness = end === areas.length ? length : Math.min(length, sum / side)
    const alongEnd = vertical ? remaining.y + remaining.height : remaining.x + remaining.width
    let cursor = vertical ? remaining.y : remaining.x

    for (let i = start; i < end; i += 1) {
      // The final item absorbs rounding residue; every endpoint stays in bounds.
      const next = i === end - 1 ? alongEnd : Math.min(alongEnd, cursor + (areas[i].area / sum) * side)
      result.push({
        id: areas[i].id,
        rect: vertical
          ? { x: remaining.x, y: cursor, width: thickness, height: Math.max(0, next - cursor) }
          : { x: cursor, y: remaining.y, width: Math.max(0, next - cursor), height: thickness },
      })
      cursor = next
    }

    remaining = vertical
      ? { ...remaining, x: remaining.x + thickness, width: Math.max(0, remaining.width - thickness) }
      : { ...remaining, y: remaining.y + thickness, height: Math.max(0, remaining.height - thickness) }
    start = end
  }

  return result
}
