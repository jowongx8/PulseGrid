import { createRef, StrictMode } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
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

it('fits only the open dialog to the visual viewport and cleans up across transitions and close', () => {
  const viewport = Object.assign(new EventTarget(), { height: 640, offsetTop: 0, scale: 1 })
  vi.stubGlobal('visualViewport', viewport)
  let callback: FrameRequestCallback = () => {}
  const request = vi.fn((next: FrameRequestCallback) => { callback = next; return 7 })
  const cancel = vi.fn()
  vi.stubGlobal('requestAnimationFrame', request)
  vi.stubGlobal('cancelAnimationFrame', cancel)
  const add = vi.spyOn(viewport, 'addEventListener')
  const remove = vi.spyOn(viewport, 'removeEventListener')
  const props = { onDismiss: vi.fn(), returnFocus: createRef<HTMLElement>(), fallbackFocus: createRef<HTMLButtonElement>(), title: 'Details', focusKey: 'a' }
  const { rerender, container } = render(<DashboardOverlay {...props} mode={null}>Content</DashboardOverlay>)
  const dialog = container.querySelector('dialog')!
  expect(add).not.toHaveBeenCalled()
  rerender(<DashboardOverlay {...props} mode="search">Content</DashboardOverlay>)
  expect(dialog.style.getPropertyValue('--overlay-height')).toBe('640px')
  expect(screen.getByRole('heading', { name: 'Details' })).toHaveFocus()
  viewport.height = 280
  viewport.offsetTop = 35
  act(() => {
    viewport.dispatchEvent(new Event('resize'))
    viewport.dispatchEvent(new Event('scroll'))
  })
  expect(request).toHaveBeenCalledOnce()
  act(() => callback(0))
  expect(dialog.style.getPropertyValue('--overlay-height')).toBe('280px')
  expect(dialog.style.getPropertyValue('--overlay-top')).toBe('35px')
  rerender(<DashboardOverlay {...props} mode="service">Content</DashboardOverlay>)
  expect(add).toHaveBeenCalledTimes(2)
  expect(native.showModal).toHaveBeenCalledOnce()
  expect(dialog.style.getPropertyValue('--overlay-height')).toBe('280px')

  viewport.scale = 2
  act(() => { viewport.dispatchEvent(new Event('resize')); callback(0) })
  expect(dialog.style.getPropertyValue('--overlay-height')).toBe('')
  expect(dialog.style.getPropertyValue('--overlay-top')).toBe('')
  viewport.scale = 1
  act(() => { viewport.dispatchEvent(new Event('resize')); callback(0) })
  expect(dialog.style.getPropertyValue('--overlay-height')).toBe('280px')
  act(() => viewport.dispatchEvent(new Event('scroll')))
  rerender(<DashboardOverlay {...props} mode={null}>Content</DashboardOverlay>)
  expect(cancel).toHaveBeenCalledWith(7)
  for (const [name, listener] of add.mock.calls) expect(remove).toHaveBeenCalledWith(name, listener)
  expect(dialog.style.getPropertyValue('--overlay-height')).toBe('')
  expect(dialog.style.getPropertyValue('--overlay-top')).toBe('')
})

it('removes visual viewport listeners and queued frames on unmount in Strict Mode', () => {
  const viewport = Object.assign(new EventTarget(), { height: 600, offsetTop: 0, scale: 1 })
  vi.stubGlobal('visualViewport', viewport)
  vi.stubGlobal('requestAnimationFrame', vi.fn(() => 9))
  const cancel = vi.fn()
  vi.stubGlobal('cancelAnimationFrame', cancel)
  const add = vi.spyOn(viewport, 'addEventListener')
  const remove = vi.spyOn(viewport, 'removeEventListener')
  const { unmount } = render(<StrictMode><DashboardOverlay mode="service" title="A" focusKey="a" onDismiss={vi.fn()} returnFocus={createRef()} fallbackFocus={createRef()}>Content</DashboardOverlay></StrictMode>)
  act(() => viewport.dispatchEvent(new Event('resize')))
  unmount()
  expect(cancel).toHaveBeenCalledWith(9)
  for (const [name, listener] of add.mock.calls) expect(remove).toHaveBeenCalledWith(name, listener)
})

it('keeps CSS fallback sizing when VisualViewport is unavailable', () => {
  vi.stubGlobal('visualViewport', undefined)
  render(<DashboardOverlay mode="service" title="A" focusKey="a" onDismiss={vi.fn()} returnFocus={createRef()} fallbackFocus={createRef()}>Content</DashboardOverlay>)
  const dialog = screen.getByRole('dialog')
  expect(dialog.style.getPropertyValue('--overlay-height')).toBe('')
  expect(dialog.style.getPropertyValue('--overlay-top')).toBe('')
  expect(dialog).toHaveAttribute('open')
})
