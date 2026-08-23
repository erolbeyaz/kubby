import { useEffect, useRef, useState, type ReactNode } from 'react'

interface CollapsibleProps {
  title: ReactNode
  defaultOpen?: boolean
  storageKey?: string
  children: ReactNode
}

/** A section that opens and closes with a real height transition, not a jump. */
export function Collapsible({ title, defaultOpen = true, storageKey, children }: CollapsibleProps) {
  const [open, setOpen] = useState(() => readStoredOpen(storageKey, defaultOpen))
  const content = useRef<HTMLDivElement>(null)
  const [height, setHeight] = useState<number | undefined>(undefined)

  // Height is measured rather than guessed, so the transition matches the content and
  // an expanded section is never clipped.
  useEffect(() => {
    const element = content.current
    if (!element) return

    const measure = () => setHeight(element.scrollHeight)
    measure()

    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [children])

  useEffect(() => {
    if (!storageKey) return
    try {
      localStorage.setItem(storageKey, open ? '1' : '0')
    } catch {
      // Storage is a convenience here, never a requirement.
    }
  }, [open, storageKey])

  return (
    <section>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex h-7 w-full items-center gap-1.5 px-2 text-left text-[var(--text-muted)] transition-colors hover:text-[var(--text-secondary)]"
        style={{ fontSize: 'var(--text-micro)' }}
      >
        <svg
          width="10"
          height="10"
          viewBox="0 0 10 10"
          aria-hidden="true"
          className="shrink-0 transition-transform duration-200"
          style={{ transform: open ? 'rotate(90deg)' : 'rotate(0deg)' }}
        >
          <path d="M3 1.5 L7 5 L3 8.5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {title}
      </button>

      <div
        className="overflow-hidden transition-[height,opacity] duration-200 ease-out"
        style={{ height: open ? height : 0, opacity: open ? 1 : 0 }}
      >
        <div ref={content}>{children}</div>
      </div>
    </section>
  )
}

function readStoredOpen(key: string | undefined, fallback: boolean): boolean {
  if (!key) return fallback
  try {
    const stored = localStorage.getItem(key)
    return stored === null ? fallback : stored === '1'
  } catch {
    return fallback
  }
}
