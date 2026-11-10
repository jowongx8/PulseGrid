import { cleanup } from '@testing-library/react'
import { vi } from 'vitest'

// jsdom has no native dialog lifecycle. This shim models lifecycle events only;
// browser checks verify top-layer rendering, inertness and keyboard containment.
export function mockDialog() {
  const prototype = HTMLDialogElement.prototype
  const originalShow = Object.getOwnPropertyDescriptor(prototype, 'showModal')
  const originalClose = Object.getOwnPropertyDescriptor(prototype, 'close')
  const showModal = vi.fn(function (this: HTMLDialogElement) { this.setAttribute('open', '') })
  const close = vi.fn(function (this: HTMLDialogElement) {
    if (!this.open) return
    this.removeAttribute('open')
    queueMicrotask(() => this.dispatchEvent(new Event('close')))
  })
  Object.defineProperty(prototype, 'showModal', { configurable: true, value: showModal })
  Object.defineProperty(prototype, 'close', { configurable: true, value: close })

  return {
    showModal, close,
    cancel(element: HTMLDialogElement) {
      if (element.dispatchEvent(new Event('cancel', { cancelable: true }))) element.close()
    },
    restore() {
      cleanup()
      if (originalShow) Object.defineProperty(prototype, 'showModal', originalShow)
      else Reflect.deleteProperty(prototype, 'showModal')
      if (originalClose) Object.defineProperty(prototype, 'close', originalClose)
      else Reflect.deleteProperty(prototype, 'close')
    },
  }
}
