import { useState } from 'react'

import { ContextMenu, type MenuItem } from './ContextMenu'

/**
 * The + that opens a dock tab, always in reach.
 *
 * The dock's own + only exists once something is already open, which is the one moment it
 * is not needed. This one sits on the status strip and is there from an empty workspace,
 * so a terminal is a click away rather than something that has to be found first.
 */
export function DockLauncher({ items }: { items: MenuItem[] }) {
  const [at, setAt] = useState<{ x: number; y: number } | null>(null)

  if (items.length === 0) return null

  return (
    <>
      <button
        type="button"
        title="New tab"
        aria-label="New tab"
        aria-haspopup="menu"
        onClick={(event) => {
          const box = event.currentTarget.getBoundingClientRect()
          // Above the button: the strip is at the bottom of the window and a menu
          // dropped below it would open off-screen.
          setAt({ x: box.left, y: box.top - 4 })
        }}
        className="flex h-7 w-7 shrink-0 items-center justify-center transition-colors hover:bg-[var(--bg-hover)]"
        style={{ borderRadius: 'var(--radius-sharp)', color: 'var(--text-secondary)' }}
      >
        <svg width="13" height="13" viewBox="0 0 16 16" aria-hidden="true">
          <path d="M8 3v10M3 8h10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
      </button>

      {at && <ContextMenu items={items} at={at} onClose={() => setAt(null)} />}
    </>
  )
}
