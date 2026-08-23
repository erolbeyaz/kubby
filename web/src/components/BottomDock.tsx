import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'

interface DockPane {
  id: string
  label: string
  /** An SVG path, drawn before the label so the strip is scannable by shape. */
  icon?: string
  render: () => ReactNode
}

interface BottomDockProps {
  tabs: DockPane[]
  activeId: string
  onSelect: (id: string) => void
  /** Closes one tab. */
  onCloseTab: (id: string) => void
  /** Closes the dock entirely. */
  onClose: () => void
  storageKey: string
}

const MIN_HEIGHT = 120
const DEFAULT_HEIGHT = 300

/**
 * A dock across the bottom of the workspace, dragged to whatever height the task needs.
 *
 * Logs, describe and events belong beside the object rather than instead of it: reading
 * a crash loop means moving between the pod's state and its output, and a full-screen
 * log view turns that into navigation.
 */
export function BottomDock({ tabs, activeId, onSelect, onCloseTab, onClose, storageKey }: BottomDockProps) {
  const [height, setHeight] = useState(() => readStored(storageKey, DEFAULT_HEIGHT))
  const [dragging, setDragging] = useState(false)
  const startY = useRef(0)
  const startHeight = useRef(0)

  const onPointerMove = useCallback((event: PointerEvent) => {
    // The dock grows upward, so a pointer moving up increases the height.
    const next = startHeight.current - (event.clientY - startY.current)
    setHeight(Math.max(MIN_HEIGHT, Math.min(window.innerHeight - 160, next)))
  }, [])

  const onPointerUp = useCallback(() => setDragging(false), [])

  useEffect(() => {
    if (!dragging) return

    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', onPointerUp)
    document.body.style.userSelect = 'none'
    document.body.style.cursor = 'row-resize'

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
      localStorage.setItem(storageKey, String(height))
    } catch {
      // Blocked storage is fine; the height simply resets next time.
    }
  }, [dragging, storageKey, height])

  const active = tabs.find((tab) => tab.id === activeId) ?? tabs[0]

  return (
    <section
      aria-label="Details dock"
      className="flex shrink-0 flex-col border-t"
      style={{ height, borderColor: 'var(--border-default)', backgroundColor: 'var(--bg-base)' }}
    >
      <div
        role="separator"
        aria-orientation="horizontal"
        aria-label="Resize dock"
        tabIndex={0}
        onPointerDown={(event) => {
          startY.current = event.clientY
          startHeight.current = height
          setDragging(true)
        }}
        onKeyDown={(event) => {
          if (event.key === 'ArrowUp') setHeight((h) => Math.min(window.innerHeight - 160, h + 24))
          if (event.key === 'ArrowDown') setHeight((h) => Math.max(MIN_HEIGHT, h - 24))
        }}
        className="-mt-1 h-2 shrink-0 cursor-row-resize"
      >
        <div
          className="pointer-events-none mt-1 h-px w-full transition-colors"
          style={{ backgroundColor: dragging ? 'var(--accent)' : 'transparent' }}
        />
      </div>

      <div
        role="tablist"
        className="flex h-8 shrink-0 items-stretch border-b"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        <div className="flex min-w-0 flex-1 items-stretch overflow-x-auto">
          {tabs.map((tab) => (
            <div
              key={tab.id}
              // Middle-click closes, the way it does in every tab strip people already use.
              onAuxClick={(event) => {
                if (event.button === 1) {
                  event.preventDefault()
                  onCloseTab(tab.id)
                }
              }}
              className="group flex shrink-0 items-center border-r transition-colors hover:bg-[var(--bg-hover)]"
              style={{
                borderColor: 'var(--border-subtle)',
                // The marker sits on the top edge: the dock's tabs are above their
                // content, so an underline would point away from what it labels.
                boxShadow: tab.id === active?.id ? 'inset 0 2px 0 0 var(--accent)' : undefined,
                backgroundColor: tab.id === active?.id ? 'var(--bg-base)' : undefined,
              }}
            >
              <button
                type="button"
                role="tab"
                aria-selected={tab.id === active?.id}
                onClick={() => onSelect(tab.id)}
                className="flex max-w-[16rem] items-center gap-1.5 py-1 pl-2.5 pr-1"
                style={{
                  fontSize: 'var(--text-secondary-size)',
                  color: tab.id === active?.id ? 'var(--text-primary)' : 'var(--text-muted)',
                }}
              >
                {tab.icon && (
                  <svg
                    // Sized in em and drawn edge to edge, so the glyph stands exactly as
                    // tall as the capital letter next to it rather than near it.
                    width="1em"
                    height="1em"
                    viewBox="0 0 16 16"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.4"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                    className="shrink-0"
                    style={{ color: tab.id === active?.id ? 'var(--accent)' : 'inherit' }}
                  >
                    <path d={tab.icon} />
                  </svg>
                )}
                <span className="truncate">{tab.label}</span>
              </button>

              <button
                type="button"
                onClick={() => onCloseTab(tab.id)}
                aria-label={`Close ${tab.label}`}
                className="mr-1 flex h-5 w-5 items-center justify-center transition-colors hover:bg-[var(--bg-active)]"
                style={{ borderRadius: 'var(--radius-sharp)', color: 'var(--text-muted)' }}
              >
                <svg width="9" height="9" viewBox="0 0 12 12" aria-hidden="true">
                  <path d="M2 2 L10 10 M10 2 L2 10" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                </svg>
              </button>
            </div>
          ))}
        </div>

        <button
          type="button"
          onClick={onClose}
          aria-label="Close dock"
          className="mr-1 flex h-8 w-8 shrink-0 items-center justify-center transition-colors hover:bg-[var(--bg-hover)]"
          style={{ color: 'var(--text-muted)' }}
        >
          <svg width="11" height="11" viewBox="0 0 12 12" aria-hidden="true">
            <path d="M2 2 L10 10 M10 2 L2 10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
      </div>

      <div className="min-h-0 flex-1">{active?.render()}</div>
    </section>
  )
}

function readStored(key: string, fallback: number): number {
  try {
    const stored = Number(localStorage.getItem(key))
    return Number.isFinite(stored) && stored >= MIN_HEIGHT ? stored : fallback
  } catch {
    return fallback
  }
}
