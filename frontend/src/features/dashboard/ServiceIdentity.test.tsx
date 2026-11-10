import { render } from '@testing-library/react'
import { expect, it } from 'vitest'
import { ServiceIdentity } from './ServiceIdentity'

it.each([['Google Drive', 'GD'], ['GitHub', 'GI'], ['  A  ', 'A'], ['', '?'], ['😀 test', '😀T']])('derives neutral initials for %s', (name, initials) => {
  const { container } = render(<ServiceIdentity name={name} size={20} />)
  const monogram = container.querySelector('[data-service-identity]')
  expect(monogram).toHaveTextContent(initials)
  expect(monogram).toHaveClass('bg-primary', 'text-primary-foreground')
  expect(monogram).toHaveAttribute('aria-hidden', 'true')
})
