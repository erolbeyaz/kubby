import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'

export interface MenuItem {
  id: string
  label: string
  icon?: ReactNode
  /** Destructive items are red and sit below a divider, away from an accidental click. */
  destructive?: boolean
  disabled?: boolean
  /** Shown next to the label when the item is unavailable, saying why. */
  note?: string
  onSelect?: () => void
}

interface ContextMenuProps {
  items: MenuItem[]
  at: { x: number; y: number }
  onClose: () => void
}

/**
 * The menu a right-click opens.
 *
 * Every entry comes from the one action registry (ADR-053), so the icon strip in the
 * detail panel and this menu can never disagree about what an object can do.
 */
export function ContextMenu({ items, at, onClose }: ContextMenuProps) {
  const container = useRef<HTMLDivElement>(null)
  const [position, setPosition] = useState(at)

  // A menu opened near an edge would otherwise be half off-screen, which is exactly
  // where a long list of actions ends up.
  useLayoutEffect(() => {
    const element = container.current
    if (!element) return

    const box = element.getBoundingClientRect()
    setPosition({
      x: Math.min(at.x, window.innerWidth - box.width - 8),
      y: Math.min(at.y, window.innerHeight - box.height - 8),
    })
  }, [at])

  useEffect(() => {
    const onPointerDown = (event: MouseEvent) => {
      if (!container.current?.contains(event.target as Node)) onClose()
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }

    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    window.addEventListener('resize', onClose)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('resize', onClose)
    }
  }, [onClose])

  const safe = items.filter((item) => !item.destructive)
  const destructive = items.filter((item) => item.destructive)

  return (
    <div
      ref={container}
      role="menu"
      className="fixed z-50 min-w-[13rem] border py-1 shadow-lg"
      style={{
        left: position.x,
        top: position.y,
        backgroundColor: 'var(--bg-overlay)',
        borderColor: 'var(--border-strong)',
        borderRadius: 'var(--radius-panel)',
      }}
    >
      {safe.map((item) => (
        <Item key={item.id} item={item} onClose={onClose} />
      ))}

      {destructive.length > 0 && safe.length > 0 && (
        <div className="my-1 h-px" style={{ backgroundColor: 'var(--border-subtle)' }} />
      )}

      {destructive.map((item) => (
        <Item key={item.id} item={item} onClose={onClose} />
      ))}
    </div>
  )
}

function Item({ item, onClose }: { item: MenuItem; onClose: () => void }) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={item.disabled}
      onClick={() => {
        item.onSelect?.()
        onClose()
      }}
      className={`menu-item${item.destructive ? ' menu-item--destructive' : ''}`}
    >
      {item.icon && <span className="flex w-4 shrink-0 justify-center">{item.icon}</span>}
      <span className="flex-1 truncate">{item.label}</span>
      {item.note && (
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>{item.note}</span>
      )}
    </button>
  )
}
