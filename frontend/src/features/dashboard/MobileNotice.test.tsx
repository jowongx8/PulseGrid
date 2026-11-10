import userEvent from '@testing-library/user-event'
import { render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { MobileNotice } from './MobileNotice'

beforeEach(() => localStorage.clear())

it('provides a compact informational notice with CSS-only narrow-screen visibility', () => {
  render(<MobileNotice onDismiss={vi.fn()} />)
  expect(screen.getByRole('complementary', { name: 'Larger-screen notice' })).toHaveClass('md:hidden', 'shrink-0')
  expect(screen.getByRole('heading', { name: 'Better on a larger screen' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Dismiss larger-screen notice' })).toBeEnabled()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

it('dismisses by keyboard, notifies the shell and persists across remounts', async () => {
  const user = userEvent.setup()
  const dismiss = vi.fn()
  const { unmount } = render(<MobileNotice onDismiss={dismiss} />)
  await user.tab()
  await user.keyboard('{Enter}')
  expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
  expect(dismiss).toHaveBeenCalledOnce()
  expect(localStorage.getItem('pulsegrid-desktop-notice-dismissed')).toBe('true')
  unmount()
  render(<MobileNotice onDismiss={dismiss} />)
  expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
})

it('remains dismissible when storage access fails', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('Storage blocked') })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('Storage blocked') })
  const dismiss = vi.fn()
  render(<MobileNotice onDismiss={dismiss} />)
  await userEvent.setup().click(screen.getByRole('button', { name: 'Dismiss larger-screen notice' }))
  expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
  expect(dismiss).toHaveBeenCalledOnce()
})
