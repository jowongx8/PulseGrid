import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { ServiceDTO, ServiceState } from '@/data/contracts'
import { makeService } from '@/test/services'
import { ServiceTile } from './ServiceTile'

function renderTile(width: number, height: number, service: ServiceDTO = makeService('example', 'up')) {
  return render(
    <ul>
      <ServiceTile
        service={service}
        placement={{
          id: service.id,
          allocation: { x: 0, y: 0, width, height },
          bounds: { x: 1.5, y: 1.5, width: width - 3, height: height - 3 },
        }}
      />
    </ul>,
  )
}

describe('ServiceTile', () => {
  it.each<[number, number, string]>([
    [96, 74, 'large'], [95, 74, 'medium'], [96, 73, 'medium'], [58, 40, 'medium'],
    [57, 40, 'small'], [58, 39, 'small'], [30, 30, 'small'], [29, 100, 'tiny'], [100, 29, 'tiny'],
  ])('uses the %s × %s allocation to choose the %s tier', (width, height, variant) => {
    renderTile(width, height)
    expect(screen.getByRole('listitem', { name: 'Service example: Operational' })).toHaveAttribute('data-variant', variant)
  })

  it.each<[ServiceState, string, string]>([
    ['up', 'Operational', 'lucide-circle-check'],
    ['down', 'Down', 'lucide-octagon-x'],
    ['unknown', 'Unknown', 'lucide-circle-help'],
  ])('renders a distinct icon and caption for %s', (state, label, icon) => {
    renderTile(150, 100, makeService('example', state))
    const tile = screen.getByRole('listitem', { name: `Service example: ${label}` })
    expect(tile).toHaveAttribute('data-status', state)
    expect(tile.querySelector('svg')).toHaveClass(icon)
    expect(screen.getByText(label)).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(tile).not.toHaveAttribute('tabindex')
  })

  it('uses a single name/icon row at the minimum Medium size', () => {
    const { container } = renderTile(58, 40)
    expect(screen.getByText('Service example')).toHaveClass('truncate')
    expect(container.querySelector('[data-service-identity]')).toBeNull()
    expect(container.querySelector('svg')).toBeInTheDocument()
    expect(screen.queryByText('Operational')).not.toBeInTheDocument()
  })

  it('keeps the Medium monogram when both rows fit', () => {
    const { container } = renderTile(80, 60)
    expect(container.querySelector('[data-service-identity]')).toBeInTheDocument()
    expect(screen.getByText('Service example')).toBeInTheDocument()
    expect(screen.queryByText('Operational')).not.toBeInTheDocument()
  })

  it.each<[number, boolean]>([[30, false], [50, true]])('fits Small content at width %s', (width, monogram) => {
    const { container } = renderTile(width, 32)
    expect(Boolean(container.querySelector('[data-service-identity]'))).toBe(monogram)
    expect(container.querySelector('svg')).toBeInTheDocument()
    expect(screen.queryByText('Service example')).not.toBeInTheDocument()
  })

  it.each(['up', 'down', 'unknown'] as const)('retains a bounded Tiny %s indicator and accessible identity', (state) => {
    const { container } = renderTile(20, 20, makeService('example', state))
    const tile = screen.getByRole('listitem')
    expect(tile).toHaveAttribute('data-status', state)
    expect(tile).toHaveAttribute('data-variant', 'tiny')
    expect(tile).toHaveAccessibleName(/Service example:/)
    expect(container.querySelector('.tiny-status')).toBeInTheDocument()
    expect(container.querySelector('svg')).toBeNull()
    expect(container.querySelector('[data-service-identity]')).toBeNull()
  })

  it('removes glyphs below their drawable size without losing accessible information', () => {
    const { container } = renderTile(8, 10, makeService('example', 'unknown'))
    const tile = screen.getByRole('listitem', { name: 'Service example: Unknown' })
    expect(tile).toHaveAttribute('title', 'Service example: Unknown')
    expect(container.querySelector('.tiny-status')).toBeNull()
    expect(container.querySelector('svg')).toBeNull()
  })

  it.each([['Google Drive', 'GD'], ['GitHub', 'GI'], ['  A  ', 'A']])('derives neutral monogram initials for %s', (name, initials) => {
    const { container } = renderTile(150, 100, { ...makeService('example', 'down'), name })
    const monogram = container.querySelector('[data-service-identity]')
    expect(monogram).toHaveTextContent(initials)
    expect(monogram).toHaveClass('bg-primary', 'text-primary-foreground')
  })

  it('preserves the full identity of a long name while allowing visible truncation', () => {
    const name = 'A service with a very long descriptive name '.repeat(8)
    renderTile(96, 74, { ...makeService('example', 'up'), name })
    const tile = screen.getByRole('listitem', { name: `${name}: Operational` })
    expect(tile).toHaveAttribute('title', `${name}: Operational`)
    expect(tile.querySelector('p')).toHaveClass('truncate')
  })
})
