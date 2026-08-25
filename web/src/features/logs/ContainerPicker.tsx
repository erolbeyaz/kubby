import { useEffect, useRef, useState } from 'react'

import type { PodContainer } from '@/lib/api'

/** Sidecars are listed but grouped after the workload's own containers (ADR-030). */
export function ContainerPicker({
  containers,
  selected,
  onChange,
}: {
  containers: PodContainer[]
  selected: string[]
  onChange: (names: string[]) => void
}) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return

    const onPointerDown = (event: MouseEvent) => {
      if (!box.current?.contains(event.target as Node)) setOpen(false)
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

  const all = selected.length === 0
  const label = all
    ? 'All containers'
    : selected.length === 1
      ? selected[0]
      : `${selected.length} containers`

  const toggle = (name: string) => {
    // Picking one while everything is streaming means "just this one", the same way the
    // namespace picker reads (ADR-051).
    if (all) {
      onChange([name])
      return
    }
    const next = selected.includes(name)
      ? selected.filter((value) => value !== name)
      : [...selected, name]
    onChange(next.length === 0 || next.length === containers.length ? [] : next)
  }

  return (
    <div ref={box} className="relative">
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label="Container"
        onClick={() => setOpen((value) => !value)}
        className="flex h-7 w-full items-center gap-2 border px-2 font-mono"
        style={{
          borderRadius: 'var(--radius-sharp)',
          borderColor: open ? 'var(--accent)' : 'var(--border-default)',
          backgroundColor: 'var(--bg-base)',
          fontSize: 'var(--text-micro)',
          color: 'var(--text-primary)',
        }}
      >
        <span className="flex-1 truncate text-left">{label}</span>
        <svg width="8" height="8" viewBox="0 0 10 10" aria-hidden="true" className="shrink-0 opacity-60">
          <path d="M2 3.5 L5 6.5 L8 3.5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      </button>

      {open && (
        <div
          role="listbox"
          aria-multiselectable="true"
          className="absolute left-0 z-50 mt-1 min-w-[13rem] border py-1 shadow-lg"
          style={{
            backgroundColor: 'var(--bg-overlay)',
            borderColor: 'var(--border-strong)',
            borderRadius: 'var(--radius-panel)',
          }}
        >
          <label
            role="option"
            aria-selected={all}
            className="mb-1 flex h-7 cursor-pointer items-center gap-2 border-b px-2 pb-1 hover:bg-[var(--bg-hover)]"
            style={{ borderColor: 'var(--border-subtle)' }}
          >
            <input type="checkbox" checked={all} aria-label="All containers" onChange={() => onChange([])} />
            <span style={{ fontSize: 'var(--text-secondary-size)', color: all ? 'var(--accent)' : 'var(--text-secondary)' }}>
              All containers
            </span>
          </label>

          {containers.map((container) => {
            const checked = all || selected.includes(container.name)
            return (
              <label
                key={container.name}
                role="option"
                aria-selected={checked}
                className="flex h-7 cursor-pointer items-center gap-2 px-2 hover:bg-[var(--bg-hover)]"
              >
                <input type="checkbox" checked={checked} onChange={() => toggle(container.name)} />
                <span
                  className="flex-1 truncate font-mono"
                  style={{
                    fontSize: 'var(--text-micro)',
                    color: checked ? 'var(--text-primary)' : 'var(--text-secondary)',
                  }}
                >
                  {container.name}
                </span>
                {container.role !== 'app' && (
                  <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                    {container.role}
                  </span>
                )}
              </label>
            )
          })}
        </div>
      )}
    </div>
  )
}
