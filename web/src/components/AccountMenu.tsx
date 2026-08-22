import { useEffect, useRef, useState } from 'react'

import { Icon, type IconName } from '@/components/Icon'
import type { Me } from '@/lib/api'

export type AccountAction = 'password' | 'mfa' | 'sessions'

interface AccountMenuProps {
  me: Me
  onSelect: (action: AccountAction) => void
  onSignOut: () => void
}

const ITEMS: readonly { id: AccountAction; label: string; icon: IconName }[] = [
  { id: 'password', label: 'Change password', icon: 'key' },
  { id: 'mfa', label: 'Two-factor authentication', icon: 'shield' },
  { id: 'sessions', label: 'Active sessions', icon: 'monitor' },
]

/** Derives the avatar text: initials from the display name, falling back to the email. */
function initials(me: Me): string {
  const source = me.user.displayName.trim() || me.user.email
  const parts = source.split(/[\s._@-]+/).filter(Boolean)

  if (parts.length >= 2) {
    return (parts[0]![0]! + parts[1]![0]!).toUpperCase()
  }
  return source.slice(0, 2).toUpperCase()
}

/** The account control in the top-right corner: avatar, settings shortcuts, sign out. */
export function AccountMenu({ me, onSelect, onSignOut }: AccountMenuProps) {
  const [open, setOpen] = useState(false)
  const container = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return

    const onPointerDown = (event: MouseEvent) => {
      if (!container.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }

    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div ref={container} className="relative">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Account: ${me.user.displayName}`}
        onClick={() => setOpen((value) => !value)}
        className="flex h-8 w-8 items-center justify-center font-semibold transition-colors"
        style={{
          borderRadius: 'var(--radius-sharp)',
          fontSize: 'var(--text-secondary-size)',
          backgroundColor: open ? 'var(--accent)' : 'var(--bg-raised)',
          color: open ? 'var(--text-inverse)' : 'var(--text-secondary)',
          border: '1px solid var(--border-default)',
        }}
      >
        {initials(me)}
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 z-50 mt-1 w-64 border shadow-lg"
          style={{
            backgroundColor: 'var(--bg-overlay)',
            borderColor: 'var(--border-strong)',
            borderRadius: 'var(--radius-panel)',
          }}
        >
          <div className="border-b px-3 py-2.5" style={{ borderColor: 'var(--border-subtle)' }}>
            <p className="truncate font-medium" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
              {me.user.displayName}
            </p>
            <p className="truncate font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
              {me.user.email}
            </p>
            <span
              className="mt-1.5 inline-block px-1.5 py-0.5 font-mono uppercase tracking-wider"
              style={{
                fontSize: 'var(--text-micro)',
                borderRadius: 'var(--radius-sharp)',
                backgroundColor: 'var(--accent-muted)',
                color: 'var(--accent)',
              }}
            >
              {me.user.role}
            </span>
          </div>

          <div className="p-1">
            {ITEMS.map((item) => (
              <button
                key={item.id}
                role="menuitem"
                type="button"
                onClick={() => {
                  onSelect(item.id)
                  setOpen(false)
                }}
                className="flex h-8 w-full items-center gap-2.5 px-2.5 text-left transition-colors hover:bg-[var(--bg-hover)]"
                style={{
                  borderRadius: 'var(--radius-sharp)',
                  fontSize: 'var(--text-secondary-size)',
                  color: 'var(--text-secondary)',
                }}
              >
                <Icon name={item.icon} />
                {item.label}
              </button>
            ))}
          </div>

          <div className="border-t p-1" style={{ borderColor: 'var(--border-subtle)' }}>
            <button
              role="menuitem"
              type="button"
              onClick={() => {
                setOpen(false)
                onSignOut()
              }}
              className="flex h-8 w-full items-center gap-2.5 px-2.5 text-left font-medium transition-colors"
              style={{
                borderRadius: 'var(--radius-sharp)',
                fontSize: 'var(--text-secondary-size)',
                color: 'var(--status-error)',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = 'color-mix(in srgb, var(--status-error) 14%, transparent)'
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = 'transparent'
              }}
            >
              <Icon name="logout" />
              Sign out
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
