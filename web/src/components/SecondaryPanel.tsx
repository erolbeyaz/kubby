import type { ReactNode } from 'react'

interface SecondaryPanelProps {
  title: string
  children: ReactNode
}

/** The context panel beside the rail: cluster picker, namespace list, resource tree. */
export function SecondaryPanel({ title, children }: SecondaryPanelProps) {
  return (
    <aside
      aria-label={title}
      className="flex w-68 shrink-0 flex-col border-r"
      style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-subtle)' }}
    >
      <header
        className="flex h-9 shrink-0 items-center border-b px-3"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <h2
          className="text-[12px] font-semibold uppercase tracking-[0.08em]"
          style={{ color: 'var(--text-muted)' }}
        >
          {title}
        </h2>
      </header>
      <div className="flex-1 overflow-y-auto">{children}</div>
    </aside>
  )
}
