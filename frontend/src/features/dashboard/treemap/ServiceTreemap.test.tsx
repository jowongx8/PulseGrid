import { act, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ServiceDTO } from '@/data/contracts'
import { makeService } from '@/test/services'
import { mockResizeObserver } from '@/test/resize-observer'
import * as layout from './layout'
import { ServiceTreemap } from './ServiceTreemap'

let observer: ReturnType<typeof mockResizeObserver>
beforeEach(() => { observer = mockResizeObserver() })

function measure(width = 800, height = 500) {
  act(() => observer.resize(screen.getByRole('group', { name: 'Service treemap' }), width, height))
}

describe('ServiceTreemap', () => {
  it('waits for measurement, then renders labelled categories and current DTOs', () => {
    render(<ServiceTreemap onSelectService={vi.fn()} services={[
      { ...makeService('developer', 'up'), category: 'developer-cloud' },
      makeService('model', 'unknown'),
    ]} />)
    expect(screen.queryByRole('listitem')).not.toBeInTheDocument()
    expect(screen.queryByText('The service map is available on wider screens.')).not.toBeInTheDocument()
    measure()
    const developer = screen.getByRole('region', { name: 'Developer & Cloud' })
    expect(within(developer).getByRole('button', { name: 'Service developer: Operational' })).toBeInTheDocument()
    const ai = screen.getByRole('region', { name: 'AI' })
    expect(within(ai).getByRole('button', { name: 'Service model: Unknown' })).toBeInTheDocument()
    expect(screen.getAllByRole('button')).toHaveLength(2)
  })

  it('keeps category names accessible when visual headers do not fit', () => {
    render(<ServiceTreemap onSelectService={vi.fn()} services={[makeService('model', 'up')]} />)
    measure(60, 50)
    expect(screen.getByRole('heading', { name: 'AI' })).toHaveClass('sr-only')
    expect(screen.getByRole('region', { name: 'AI' })).toBeInTheDocument()
  })

  it.each([304, 359, 414, 624, 751, 752, 1008, 1424])('uses the same host and layout at container width %s', (width) => {
    render(<ServiceTreemap onSelectService={vi.fn()} services={[makeService('model', 'up')]} />)
    const host = screen.getByRole('group', { name: 'Service treemap' }).parentElement
    expect(host).not.toHaveClass('hidden', 'md:block')
    measure(width, 400)
    expect(screen.getByRole('button', { name: 'Service model: Operational' })).toBeInTheDocument()
  })

  it('reuses geometry and tile identity for status, timestamp, name and input-order changes', () => {
    const calculate = vi.spyOn(layout, 'createTreemapLayout')
    const services = [makeService('a', 'up'), makeService('b', 'down')]
    const { rerender } = render(<ServiceTreemap onSelectService={vi.fn()} services={services} />)
    measure()
    const calls = calculate.mock.calls.length
    const tile = screen.getByRole('button', { name: 'Service a: Operational' })
    const position = tile.closest('li')?.getAttribute('style')

    rerender(<ServiceTreemap onSelectService={vi.fn()} services={services.map((service) => ({
      ...service,
      name: `Renamed ${service.id}`,
      status: { state: 'unknown' as const, lastCheckedAt: '2026-11-10T12:00:00.123456789Z' },
    })).reverse()} />)

    expect(calculate).toHaveBeenCalledTimes(calls)
    expect(screen.getByRole('button', { name: 'Renamed a: Unknown' })).toBe(tile)
    expect(tile.closest('li')?.getAttribute('style')).toBe(position)
    expect(tile.closest('li')).toHaveAttribute('data-status', 'unknown')
    expect(within(tile).getByText('Renamed a')).toBeInTheDocument()
  })

  it('recalculates on changed integer dimensions, without remounting tiles', () => {
    const calculate = vi.spyOn(layout, 'createTreemapLayout')
    render(<ServiceTreemap onSelectService={vi.fn()} services={[makeService('model', 'up')]} />)
    measure(800.5, 500.2)
    const tile = screen.getByRole('listitem')
    const oldStyle = tile.getAttribute('style')
    const calls = calculate.mock.calls.length
    measure(800.9, 500.8)
    expect(calculate).toHaveBeenCalledTimes(calls)
    measure(600, 400)
    expect(calculate).toHaveBeenCalledTimes(calls + 1)
    expect(screen.getByRole('listitem')).toBe(tile)
    expect(tile.getAttribute('style')).not.toBe(oldStyle)
    measure(0, 0)
    expect(screen.queryByRole('listitem')).not.toBeInTheDocument()
    measure(600, 400)
    expect(screen.getByRole('listitem')).toBeInTheDocument()
  })

  it('recalculates for service weight, category and membership changes', () => {
    const calculate = vi.spyOn(layout, 'createTreemapLayout')
    const original = makeService('model', 'up')
    const { rerender } = render(<ServiceTreemap onSelectService={vi.fn()} services={[original]} />)
    measure()
    const changes: ServiceDTO[][] = [
      [{ ...original, weight: 2 }],
      [{ ...original, category: 'gaming' }],
      [original, makeService('added', 'down')],
      [original],
    ]
    for (const services of changes) {
      const calls = calculate.mock.calls.length
      rerender(<ServiceTreemap onSelectService={vi.fn()} services={services} />)
      expect(calculate).toHaveBeenCalledTimes(calls + 1)
      expect(screen.getAllByRole('listitem')).toHaveLength(services.length)
    }
  })

  it('distinguishes empty data from unusable geometry', () => {
    const { rerender } = render(<ServiceTreemap onSelectService={vi.fn()} services={[]} />)
    measure()
    expect(screen.getByText('No services to display')).toBeInTheDocument()
    rerender(<ServiceTreemap onSelectService={vi.fn()} services={[{ ...makeService('model', 'up'), weight: NaN }]} />)
    expect(screen.getByText('Service map unavailable for this data')).toBeInTheDocument()
    rerender(<ServiceTreemap onSelectService={vi.fn()} services={[makeService('duplicate', 'up'), makeService('duplicate', 'down')]} />)
    expect(screen.getByText('Service map unavailable for this data')).toBeInTheDocument()
  })
})
