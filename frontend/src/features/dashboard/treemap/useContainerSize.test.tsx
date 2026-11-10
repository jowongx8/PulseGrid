import { StrictMode } from 'react'
import { act, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { mockResizeObserver } from '@/test/resize-observer'
import { useContainerSize } from './useContainerSize'

let observer: ReturnType<typeof mockResizeObserver>
beforeEach(() => { observer = mockResizeObserver() })

function Probe() {
  const { ref, size } = useContainerSize()
  return <div ref={ref} aria-label="Measured container">{size.width} × {size.height}</div>
}

describe('useContainerSize', () => {
  it('starts unmeasured and floors observed content-box dimensions', () => {
    render(<Probe />)
    const container = screen.getByLabelText('Measured container')
    expect(container).toHaveTextContent('0 × 0')
    act(() => observer.resize(container, 300.8, 150.9))
    expect(container).toHaveTextContent('300 × 150')
    act(() => observer.resize(container, 300.1, 150.1))
    expect(container).toHaveTextContent('300 × 150')
    act(() => observer.resize(container, 0, 0))
    expect(container).toHaveTextContent('0 × 0')
    act(() => observer.resize(container, 500, 200))
    expect(container).toHaveTextContent('500 × 200')
  })

  it('handles invalid measurements without emitting negative or non-finite sizes', () => {
    render(<Probe />)
    const container = screen.getByLabelText('Measured container')
    act(() => observer.resize(container, NaN, -100))
    expect(container).toHaveTextContent('0 × 0')
    act(() => observer.resize(container, Infinity, 50))
    expect(container).toHaveTextContent('0 × 50')
  })

  it('disconnects through Strict Mode remounts and ignores late callbacks', () => {
    const { unmount } = render(<StrictMode><Probe /></StrictMode>)
    const container = screen.getByLabelText('Measured container')
    expect(observer.instances.filter((instance) => instance.elements.has(container))).toHaveLength(1)
    act(() => observer.resize(container, 100, 50))
    expect(container).toHaveTextContent('100 × 50')
    unmount()
    for (const instance of observer.instances) {
      expect(instance.disconnect).toHaveBeenCalledOnce()
      expect(instance.elements.size).toBe(0)
      act(() => instance.emit(container, 900, 600))
    }
    expect(screen.queryByLabelText('Measured container')).not.toBeInTheDocument()
  })
})
