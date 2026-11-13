import userEvent from '@testing-library/user-event'
import { fireEvent, render, screen } from '@testing-library/react'
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
  it.each(['spotify', 'instagram'])('shows %s in compact and large tiles', (id) => {
    const service = makeService(id, 'up')
    const { container, unmount } = renderTile(80, 60, service)
    expect(container.querySelector('img')).toHaveAttribute('src', `/logos/${id}.svg`)
    unmount()
    const large = renderTile(150, 100, service)
    expect(large.container.querySelector('img')).toHaveAttribute('src', `/logos/${id}.svg`)
  })

  it('keeps click and keyboard activation after a logo fails', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    const { container } = renderTile(150, 100, { ...makeService('github', 'down'), name: 'GitHub' }, onSelect)
    const button = screen.getByRole('button', { name: 'GitHub: Down' })
    expect(container.querySelector('img')).toHaveAttribute('src', '/logos/github.svg')
    await user.click(button)
    fireEvent.error(container.querySelector('img')!)
    expect(container.querySelector('[data-service-identity="monogram"]')).toHaveTextContent('GI')
    await user.keyboard('{Enter} ')
    expect(onSelect).toHaveBeenCalledTimes(3)
    expect(onSelect).toHaveBeenLastCalledWith('github', button)
    expect(button.closest('li')).toHaveAttribute('data-status', 'down')
  })

  it.each([
    [200, 180, true], [96, 74, true], [80, 60, true], [58, 40, false],
    [50, 32, false], [30, 30, false], [20, 20, false], [100, 29, false],
    [300, 40, false], [40, 300, false],
  ])('fits or omits the logo in a %s × %s tile', (width, height, visible) => {
    const { container } = renderTile(width, height, makeService('github', 'up'))
    const image = container.querySelector('img')
    expect(Boolean(image)).toBe(visible)
    if (image) {
      expect(image).toHaveAttribute('src', '/logos/github.svg')
      const size = Number.parseFloat(image.parentElement!.style.width)
      expect(size).toBeGreaterThanOrEqual(20)
      expect(size).toBeLessThanOrEqual(Math.min(64, width - 11, height - 30))
      expect(image.parentElement!.style.height).toBe(`${size}px`)
    }
    expect(screen.getByRole('button')).toHaveAccessibleName('Service github: Operational')
  })

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

  it('centers a shared identity above the name as a balanced group', () => {
    const { container } = renderTile(150, 100, { ...makeService('github', 'up'), name: 'GitHub' })
    const identity = container.querySelector('[data-service-identity]')!
    expect(identity.parentElement).toHaveClass('flex-col', 'items-center', 'justify-center')
    expect(identity.nextElementSibling).toHaveTextContent('GitHub')
    expect(screen.getByRole('button')).toHaveClass('text-center')
  })

  it.each([[58, 40], [50, 32], [300, 29]])('prioritizes the name over a logo at %s × %s', (width, height) => {
    const { container } = renderTile(width, height)
    expect(screen.getByText('Service example')).toBeInTheDocument()
    expect(container.querySelector('[data-service-identity]')).toBeNull()
  })

  it('keeps short names readable even in narrow tiles', () => {
    const { container } = renderTile(30, 30, { ...makeService('x', 'up'), name: 'X' })
    expect(screen.getByText('X')).toHaveStyle({ fontSize: '11px' })
    expect(container.querySelector('[data-service-identity]')).toBeNull()
  })

  it('uses the same centered placement for missing logos', () => {
    const { container } = renderTile(150, 100)
    const identity = container.querySelector('[data-service-identity="monogram"]')!
    expect(identity.parentElement).toHaveClass('items-center', 'justify-center')
    expect(identity.nextElementSibling).toHaveTextContent('Service example')
  })

  it('grows logos with available dimensions and caps their size', () => {
    const sizes = [[80, 60], [150, 100], [200, 180], [400, 400]].map(([width, height]) => {
      const { container, unmount } = renderTile(width, height)
      const size = Number.parseFloat((container.querySelector('[data-service-identity]') as HTMLElement).style.width)
      unmount()
      return size
    })
    expect(sizes[0]).toBeLessThan(sizes[1])
    expect(sizes[1]).toBeLessThan(sizes[2])
    expect(sizes[2]).toBe(sizes[3])
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
