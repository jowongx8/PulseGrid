import { useLayoutEffect, useRef, useState } from 'react'

export function useContainerSize() {
  const ref = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ width: 0, height: 0 })

  useLayoutEffect(() => {
    const element = ref.current
    if (!element) return
    let active = true
    const observer = new ResizeObserver((entries) => {
      if (!active) return
      const entry = entries.find((entry) => entry.target === element)
      if (!entry) return
      const { width, height } = entry.contentRect
      const next = {
        width: Number.isFinite(width) ? Math.max(0, Math.floor(width)) : 0,
        height: Number.isFinite(height) ? Math.max(0, Math.floor(height)) : 0,
      }
      setSize((previous) => previous.width === next.width && previous.height === next.height ? previous : next)
    })
    observer.observe(element)
    return () => {
      active = false
      observer.disconnect()
    }
  }, [])

  return { ref, size }
}
