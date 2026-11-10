import userEvent from '@testing-library/user-event'
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { ServiceDTO, ServiceState } from '@/data/contracts'
import { makeService } from '@/test/services'
import { ServiceTile } from './ServiceTile'

function renderTile(width: number, height: number, service: ServiceDTO = makeService('example', 'up'), onSelect = vi.fn()) {
  return render(
    <ul>
      <ServiceTile
        onSelect={onSelect}
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
  it('activates the same stable service ID with Enter and Space', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    renderTile(20, 20, makeService('example', 'unknown'), onSelect)
    const button = screen.getByRole('button', { name: 'Service example: Unknown' })
    button.focus()
    await user.keyboard('{Enter} ')
    expect(onSelect).toHaveBeenCalledTimes(2)
    expect(onSelect).toHaveBeenLastCalledWith('example', button)
  })

  it.each<[number, number, string]>([
    [96, 74, 'large'], [95, 74, 'medium'], [96, 73, 'medium'], [58, 40, 'medium'],
    [57, 40, 'small'], [58, 39, 'small'], [30, 30, 'small'], [29, 100, 'tiny'], [100, 29, 'tiny'],
  ])('uses the %s × %s allocation to choose the %s tier', (width, height, variant) => {
    renderTile(width, height)
    expect(screen.getByRole('button', { name: 'Service example: Operational' }).closest('li')).toHaveAttribute('data-variant', variant)
  })

  it.each<[ServiceState, string, string]>([
    ['up', 'Operational', 'lucide-circle-check'],
    ['down', 'Down', 'lucide-octagon-x'],
    ['unknown', 'Unknown', 'lucide-circle-help'],
  ])('renders a distinct icon and caption for %s', (state, label, icon) => {
    renderTile(150, 100, makeService('example', state))
    const tile = screen.getByRole('button', { name: `Service example: ${label}` }).closest('li')!
    expect(tile).toHaveAttribute('data-status', state)
    expect(tile.querySelector('svg')).toHaveClass(icon)
    expect(screen.getByText(label)).toBeInTheDocument()
    expect(screen.getByRole('button')).toHaveAccessibleName(`Service example: ${label}`)
    expect(tile).not.toHaveAttribute('tabindex')
  })

  it('uses identity/status instead of a meaningless name at the minimum Medium size', () => {
    const { container } = renderTile(58, 40)
    expect(screen.queryByText('Service example')).not.toBeInTheDocument()
    expect(container.querySelector('[data-service-identity]')).toBeInTheDocument()
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

  it.each([
    [78, 40, false], [79, 40, true], // Compact Medium name: 40px after padding, gap and icon.
    [68, 32, false], [69, 32, true], // Small name: reduced padding, same readable width.
    [60, 57, false], [61, 58, true], // Medium two-row layout also needs readable width and height.
  ])('fits a readable name at %s × %s: %s', (width, height, visible) => {
    renderTile(width, height)
    expect(Boolean(screen.queryByText('Service example'))).toBe(visible)
    expect(screen.getByRole('button')).toHaveAccessibleName('Service example: Operational')
  })

  it.each([
    [96, 74, 12], [79, 40, 11], [131, 40, 11.5], [180, 40, 12], [100, 32, 11],
  ])('bounds the %s × %s name font to %spx without text measurement', (width, height, fontSize) => {
    renderTile(width, height)
    const name = screen.getByText('Service example')
    expect(name).toHaveClass('truncate')
    expect(name).toHaveStyle({ fontSize: `${fontSize}px` })
  })

  it.each(['up', 'down', 'unknown'] as const)('retains a bounded Tiny %s indicator and accessible identity', (state) => {
    const { container } = renderTile(20, 20, makeService('example', state))
    const tile = screen.getByRole('listitem')
    expect(tile).toHaveAttribute('data-status', state)
    expect(tile).toHaveAttribute('data-variant', 'tiny')
    expect(screen.getByRole('button')).toHaveAccessibleName(/Service example:/)
    expect(container.querySelector('.tiny-status')).toBeInTheDocument()
    expect(container.querySelector('svg')).toBeNull()
    expect(container.querySelector('[data-service-identity]')).toBeNull()
  })

  it('removes glyphs below their drawable size without losing accessible information', () => {
    const { container } = renderTile(8, 10, makeService('example', 'unknown'))
    const tile = screen.getByRole('button', { name: 'Service example: Unknown' })
    expect(tile).toHaveAttribute('title', 'Service example: Unknown')
    expect(container.querySelector('.tiny-status')).toBeNull()
    expect(container.querySelector('svg')).toBeNull()
  })

  it('preserves the full identity of a long name while allowing visible truncation', () => {
    const name = 'A service with a very long descriptive name '.repeat(8)
    renderTile(96, 74, { ...makeService('example', 'up'), name })
    const tile = screen.getByRole('button', { name: `${name}: Operational` })
    expect(tile).toHaveAttribute('title', `${name}: Operational`)
    expect(tile.querySelector('p')).toHaveClass('truncate')
  })
})
