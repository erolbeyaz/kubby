import { useEffect, useRef, useState, type ReactNode } from 'react'

interface VirtualRowsProps<T> {
  items: readonly T[]
  rowHeight: number
  /** Rows rendered beyond the viewport, so scrolling does not reveal blank space. */
  overscan?: number
  header?: ReactNode
  children: (item: T, index: number) => ReactNode
}

/**
 * Renders only the rows in view.
 *
 * A namespace with a few thousand pods is ordinary, and a table that mounts every row
 * becomes unusable long before the data does. Row height is fixed, which is what makes
 * the arithmetic exact and the scrollbar honest.
 */
export function VirtualRows<T>({
  items,
  rowHeight,
  overscan = 8,
  header,
  children,
}: VirtualRowsProps<T>) {
  const viewport = useRef<HTMLDivElement>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [height, setHeight] = useState(0)

  useEffect(() => {
    const element = viewport.current
    if (!element) return

    setHeight(element.clientHeight)

    // Falling back to a static height is far better than throwing: without the guard a
    // missing ResizeObserver takes the whole table down rather than costing it the
    // ability to react to a resize.
    if (typeof ResizeObserver === 'undefined') {
      const onResize = () => setHeight(element.clientHeight)
      window.addEventListener('resize', onResize)
      return () => window.removeEventListener('resize', onResize)
    }

    const observer = new ResizeObserver(() => setHeight(element.clientHeight))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  const first = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan)
  // jsdom and a not-yet-measured viewport both report zero height; render a usable
  // window rather than nothing at all.
  const effectiveHeight = height > 0 ? height : rowHeight * 20
  const visibleCount = Math.ceil(effectiveHeight / rowHeight) + overscan * 2
  const last = Math.min(items.length, first + visibleCount)

  const visible = items.slice(first, last)

  return (
    <div
      ref={viewport}
      className="h-full overflow-auto"
      onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
    >
      {header}
      <div style={{ height: items.length * rowHeight, position: 'relative' }}>
        <div style={{ transform: `translateY(${first * rowHeight}px)` }}>
          {visible.map((item, index) => children(item, first + index))}
        </div>
      </div>
    </div>
  )
}
