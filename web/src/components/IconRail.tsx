import { Icon, type IconName } from './Icon'

export interface RailItem {
  id: string
  label: string
  icon: IconName
}

interface IconRailProps {
  items: readonly RailItem[]
  activeId: string
  onSelect: (id: string) => void
}

/** The primary navigation: icons only, always visible, keyboard reachable. */
export function IconRail({ items, activeId, onSelect }: IconRailProps) {
  return (
    <nav
      aria-label="Primary"
      className="flex w-12 shrink-0 flex-col items-center gap-0.5 border-r py-1"
      style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-subtle)' }}
    >
      {items.map((item) => {
        const active = item.id === activeId
        return (
          <button
            key={item.id}
            type="button"
            title={item.label}
            aria-label={item.label}
            aria-current={active ? 'page' : undefined}
            onClick={() => onSelect(item.id)}
            className="relative flex h-9 w-9 items-center justify-center transition-colors"
            style={{
              borderRadius: 'var(--radius-sharp)',
              color: active ? 'var(--accent)' : 'var(--text-muted)',
              backgroundColor: active ? 'var(--accent-muted)' : 'transparent',
            }}
          >
            {active && (
              <span
                aria-hidden="true"
                className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2"
                style={{ backgroundColor: 'var(--accent)' }}
              />
            )}
            <Icon name={item.icon} />
          </button>
        )
      })}
    </nav>
  )
}
