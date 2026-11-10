import { createRef, StrictMode } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mockDialog } from '@/test/dialog'
import { DashboardOverlay } from './DashboardOverlay'

let native: ReturnType<typeof mockDialog>
beforeEach(() => { native = mockDialog() })
afterEach(() => native.restore())

it('keeps one modal open through content changes and ignores stale close events', () => {
  const dismiss = vi.fn()
  const returnFocus = createRef<HTMLElement>()
  const fallbackFocus = createRef<HTMLButtonElement>()
  const props = { onDismiss: dismiss, returnFocus, fallbackFocus }
  const { rerender } = render(
    <StrictMode>
      <DashboardOverlay {...props} mode={null} title="" focusKey="none">null</DashboardOverlay>
    </StrictMode>,
  )
  rerender(<StrictMode><DashboardOverlay {...props} mode="search" title="Search services" focusKey="search"><input aria-label="Query" data-initial-focus /></DashboardOverlay></StrictMode>)
  const dialog = screen.getByRole('dialog')
  expect(screen.getByRole('textbox')).toHaveFocus()
  expect(native.showModal).toHaveBeenCalledOnce()
  rerender(<StrictMode><DashboardOverlay {...props} mode="service" title="Alpha" focusKey="a"><p>Details</p></DashboardOverlay></StrictMode>)
  expect(screen.getByRole('dialog', { name: 'Alpha' })).toBe(dialog)
  expect(screen.getByRole('heading', { name: 'Alpha' })).toHaveFocus()
  expect(native.close).not.toHaveBeenCalled()
  expect(native.showModal).toHaveBeenCalledOnce()
  fireEvent(dialog, new Event('close'))
  expect(dismiss).not.toHaveBeenCalled()
  // A normal data update must not steal focus back from browser chrome.
  const heading = screen.getByRole('heading', { name: 'Alpha' })
  heading.blur()
  rerender(<StrictMode><DashboardOverlay {...props} mode="service" title="Alpha updated" focusKey="a"><p>Updated details</p></DashboardOverlay></StrictMode>)
  expect(heading).not.toHaveFocus()
})
