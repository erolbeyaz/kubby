import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'

interface ResizablePanelProps {
  storageKey: string
  defaultWidth: number
  minWidth?: number
  maxWidth?: number
  /** Which edge the panel is anchored to, which decides where the drag handle sits. */
  side?: 'left' | 'right'
  children: ReactNode
}

/**
 * A side panel the user can drag to resize.
 *
 * The width is remembered per panel: a layout someone adjusted should still be that way
 * next time, and re-adjusting it on every visit is the kind of small friction that
 * makes a tool tiring.
 */
export function ResizablePanel({
  storageKey,
  defaultWidth,
  minWidth = 180,
  maxWidth = 560,
  side = 'left',
  children,
}: ResizablePanelProps) {
  const [width, setWidth] = useState(() => readStoredWidth(storageKey, defaultWidth))
  const [dragging, setDragging] = useState(false)
  const startX = useRef(0)
  const startWidth = useRef(0)

  const onPointerMove = useCallback(
    (event: PointerEvent) => {
      // A right-anchored panel grows as the pointer moves left, so the delta is inverted.
      const travel = event.clientX - startX.current
      const delta = side === 'right' ? -travel : travel
      setWidth(Math.min(maxWidth, Math.max(minWidth, startWidth.current + delta)))
    },
    [minWidth, maxWidth, side],
  )

  const onPointerUp = useCallback(() => setDragging(false), [])

  useEffect(() => {
    if (!dragging) return

    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', onPointerUp)
    // A drag over text would otherwise select it, which makes resizing feel broken.
    document.body.style.userSelect = 'none'
    document.body.style.cursor = 'col-resize'

    return () => {
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', onPointerUp)
      document.body.style.userSelect = ''
      document.body.style.cursor = ''
    }
  }, [dragging, onPointerMove, onPointerUp])

  useEffect(() => {
    if (dragging) return
    try {
      localStorage.setItem(storageKey, String(width))
    } catch {
      // Private browsing and blocked storage are fine; the width simply resets.
    }
  }, [dragging, storageKey, width])

  const grow = side === 'right' ? 'ArrowLeft' : 'ArrowRight'

  const handle = (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize panel"
      tabIndex={0}
      onPointerDown={(event) => {
        startX.current = event.clientX
        startWidth.current = width
        setDragging(true)
      }}
      onKeyDown={(event) => {
        // Keyboard resizing, because this tool is meant to be usable without a mouse.
        if (event.key === grow) setWidth((w) => Math.min(maxWidth, w + 16))
        if (event.key === (grow === 'ArrowLeft' ? 'ArrowRight' : 'ArrowLeft')) {
          setWidth((w) => Math.max(minWidth, w - 16))
        }
      }}
      // The hit area is wider than the visible line: a 1px target is a fussy thing to
      // catch with a mouse, and this handle gets dragged often.
      className="relative z-10 -mx-1 w-2 shrink-0 cursor-col-resize"
    >
      <div
        className="pointer-events-none mx-auto h-full w-px transition-colors"
        style={{ backgroundColor: dragging ? 'var(--accent)' : 'var(--border-subtle)' }}
      />
    </div>
  )

  return (
    <div className="flex h-full shrink-0" style={{ width }}>
      {side === 'right' && handle}
      <div className="min-w-0 flex-1 overflow-hidden">{children}</div>
      {side === 'left' && handle}
    </div>
  )
}

function readStoredWidth(key: string, fallback: number): number {
  try {
    const stored = Number(localStorage.getItem(key))
    return Number.isFinite(stored) && stored > 0 ? stored : fallback
  } catch {
    return fallback
  }
}
