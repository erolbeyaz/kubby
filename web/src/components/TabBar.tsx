export interface WorkspaceTab {
  id: string
  label: string
  detail?: string
}

interface TabBarProps {
  tabs: readonly WorkspaceTab[]
  activeId: string
  onSelect: (id: string) => void
}

/** Tabbed workspace: each tab is a cluster/namespace/resource context. */
export function TabBar({ tabs, activeId, onSelect }: TabBarProps) {
  return (
    <div
      role="tablist"
      aria-label="Workspace tabs"
      className="flex h-9 shrink-0 items-stretch border-b"
      style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-subtle)' }}
    >
      {tabs.map((tab) => {
        const active = tab.id === activeId
        return (
          <button
            key={tab.id}
            role="tab"
            type="button"
            aria-selected={active}
            onClick={() => onSelect(tab.id)}
            className="flex items-center gap-1.5 border-r px-3 text-[13px] transition-colors"
            style={{
              borderColor: 'var(--border-subtle)',
              backgroundColor: active ? 'var(--bg-base)' : 'transparent',
              color: active ? 'var(--text-primary)' : 'var(--text-muted)',
              boxShadow: active ? 'inset 0 -1px 0 0 var(--accent)' : undefined,
            }}
          >
            <span>{tab.label}</span>
            {tab.detail && (
              <span className="font-mono text-[12px]" style={{ color: 'var(--text-muted)' }}>
                {tab.detail}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
