import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { ServiceIdentity } from './ServiceIdentity'

it.each([['Google Drive', 'GD'], ['GitHub', 'GI'], ['  A  ', 'A'], ['', '?'], ['😀 test', '😀T']])('derives neutral initials for %s', (name, initials) => {
  const { container } = render(<ServiceIdentity serviceId="unlisted" name={name} size={20} />)
  const monogram = container.querySelector('[data-service-identity]')
  expect(monogram).toHaveTextContent(initials)
  expect(monogram).toHaveClass('bg-primary', 'text-primary-foreground')
  expect(monogram).toHaveAttribute('aria-hidden', 'true')
})

it('looks up a logo by stable ID, independently of its display name', () => {
  const { container } = render(<ServiceIdentity serviceId="openai" name="ChatGPT" size={44} />)
  const image = container.querySelector('img')!
  expect(image).toHaveAttribute('src', '/logos/openai.svg')
  expect(image).toHaveAttribute('alt', '')
  expect(image).toHaveClass('object-contain')
  expect(image.parentElement).toHaveAttribute('aria-hidden', 'true')
  expect(image.parentElement).toHaveClass('bg-white')
  expect(image.parentElement).toHaveStyle({ width: '44px', height: '44px', colorScheme: 'light' })
  expect(screen.queryByRole('img')).not.toBeInTheDocument()
})

it('shows the compact Amazon logo and retains initials when it fails', () => {
  const { container } = render(<ServiceIdentity serviceId="amazon" name="Amazon" size={18} />)
  expect(container.querySelector('img')).toHaveAttribute('src', '/logos/amazon.svg')
  fireEvent.error(container.querySelector('img')!)
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('[data-service-identity="monogram"]')).toHaveTextContent('AM')
})

it('replaces a failed image with initials without changing the reserved space', () => {
  const { container, rerender } = render(<ServiceIdentity serviceId="github" name="GitHub" size={20} />)
  const frame = container.querySelector('[data-service-identity]')!
  fireEvent.error(container.querySelector('img')!)
  expect(frame).toHaveAttribute('data-service-identity', 'monogram')
  expect(frame).toHaveTextContent('GI')
  expect(frame).toHaveClass('bg-primary', 'text-primary-foreground')
  expect(frame).toHaveStyle({ width: '20px', height: '20px' })
  expect(container.querySelector('img')).toBeNull()
  rerender(<ServiceIdentity serviceId="github" name="GitHub renamed" size={44} />)
  expect(container.querySelector('img')).toBeNull()
  expect(frame).toHaveTextContent('GR')
})

it('attempts the new logo when a reused presentation changes service after an error', () => {
  const { container, rerender } = render(<ServiceIdentity serviceId="github" name="GitHub" size={20} />)
  fireEvent.error(container.querySelector('img')!)
  rerender(<ServiceIdentity serviceId="discord" name="Discord" size={20} />)
  expect(container.querySelector('img')).toHaveAttribute('src', '/logos/discord.svg')
  expect(container.querySelector('[data-service-identity]')).toHaveAttribute('data-service-identity', 'logo')
})

it('shows the local Lazada campaign artwork and falls back if it fails', () => {
  const { container, rerender } = render(<ServiceIdentity serviceId="lazada-my" name="Lazada Malaysia" size={18} />)
  for (const size of [18, 20, 36, 44]) {
    rerender(<ServiceIdentity serviceId="lazada-my" name="Lazada Malaysia" size={size} />)
    expect(container.querySelector('img')).toHaveAttribute('src', '/logos/lazada-my.png')
    expect(container.querySelector('img')).toHaveAttribute('width', String(size - 4))
  }
  fireEvent.error(container.querySelector('img')!)
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('[data-service-identity="monogram"]')).toHaveTextContent('LM')
})

it.each(['spotify', 'instagram'])('shows the %s logo at compact, tile and details sizes', (serviceId) => {
  const { container, rerender } = render(<ServiceIdentity serviceId={serviceId} name={serviceId} size={18} />)
  for (const size of [18, 20, 36, 44]) {
    rerender(<ServiceIdentity serviceId={serviceId} name={serviceId} size={size} />)
    expect(container.querySelector('img')).toHaveAttribute('src', `/logos/${serviceId}.svg`)
    expect(container.querySelector('img')).toHaveAttribute('width', String(size - 4))
    expect(container.querySelector('[data-service-identity]')).toHaveStyle({ padding: '2px' })
  }
})


it.each([['roblox', 'Roblox'], ['apple-music', 'Apple Music']])('renders the local %s logo and falls back after failure', (serviceId, name) => {
  const { container } = render(<ServiceIdentity serviceId={serviceId} name={name} size={20} />)
  expect(container.querySelector('img')).toHaveAttribute('src', `/logos/${serviceId}.svg`)
  fireEvent.error(container.querySelector('img')!)
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('[data-service-identity]')).toHaveAttribute('data-service-identity', 'monogram')
})
