import { vi } from 'vitest'

export function mockResizeObserver() {
  const instances: ControlledResizeObserver[] = []

  class ControlledResizeObserver implements ResizeObserver {
    readonly elements = new Set<Element>()
    readonly callback: ResizeObserverCallback
    observe = vi.fn((element: Element) => { this.elements.add(element) })
    unobserve = vi.fn((element: Element) => { this.elements.delete(element) })
    disconnect = vi.fn(() => { this.elements.clear() })

    constructor(callback: ResizeObserverCallback) {
      this.callback = callback
      instances.push(this)
    }

    emit(element: Element, width: number, height: number) {
      const entry = { target: element, contentRect: { width, height } } as ResizeObserverEntry
      this.callback([entry], this)
    }
  }

  vi.stubGlobal('ResizeObserver', ControlledResizeObserver)
  return {
    instances,
    resize(element: Element, width: number, height: number) {
      for (const observer of instances) {
        if (observer.elements.has(element)) observer.emit(element, width, height)
      }
    },
  }
}
