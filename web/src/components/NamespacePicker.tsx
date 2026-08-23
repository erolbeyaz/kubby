import { useEffect, useRef, useState } from 'react'

interface NamespacePickerProps {
  namespaces: string[]
  selected: string[]
  onChange: (selected: string[]) => void
}

/**
 * Namespace selection, allowing several at once.
 *
 * A service rarely lives in exactly one namespace, and forcing a single choice means
 * either flipping between them or falling back to "all" and losing the narrowing.
 */
export function NamespacePicker({ namespaces, selected, onChange }: NamespacePickerProps) {
  const [open, setOpen] = useState(false)
  const [filter, setFilter] = useState('')
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

  const label =
    selected.length === 0
      ? 'All namespaces'
      : selected.length === 1
        ? selected[0]
        : `${selected.length} namespaces`

  const visible = filter
    ? namespaces.filter((name) => name.toLowerCase().includes(filter.toLowerCase()))
    : namespaces

  // An empty selection means every namespace, so "all" is the ticked state rather than
  // the absence of one — otherwise the default view looks like nothing is selected.
  const all = selected.length === 0
  const isChecked = (name: string) => all || selected.includes(name)

  const toggle = (name: string) => {
    // Picking a namespace while everything is in scope means "just this one", not
    // "everything except this one": narrowing down is what the click is for, and the
    // other reading would make the user untick every namespace to reach one.
    if (all) {
      onChange([name])
      return
    }

    const next = selected.includes(name)
      ? selected.filter((n) => n !== name)
      : [...selected, name]

    // Ticking the last one back is the same view as "all"; keeping it as an explicit
    // list would freeze the set as it is now and hide namespaces created later.
    // Unticking the last one leaves nothing to show, so it also means "all".
    onChange(next.length === 0 || next.length === namespaces.length ? [] : next)
  }

  return (
    <div ref={container} className="relative w-full">
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label="Namespace"
        onClick={() => setOpen((value) => !value)}
        className="flex h-8 w-full items-center gap-2 border px-2.5"
        style={{
          borderRadius: 'var(--radius-sharp)',
          borderColor: open ? 'var(--accent)' : 'var(--border-default)',
          backgroundColor: 'var(--bg-base)',
          fontSize: 'var(--text-secondary-size)',
          color: selected.length > 0 ? 'var(--text-primary)' : 'var(--text-secondary)',
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
          className="absolute left-0 z-50 mt-1 w-64 border shadow-lg"
          style={{
            backgroundColor: 'var(--bg-overlay)',
            borderColor: 'var(--border-strong)',
            borderRadius: 'var(--radius-panel)',
          }}
        >
          <div className="border-b p-1.5" style={{ borderColor: 'var(--border-subtle)' }}>
            <input
              autoFocus
              placeholder="Find a namespace…"
              aria-label="Find a namespace"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="h-7 w-full border px-2 outline-none focus:border-[var(--accent)]"
              style={{
                fontSize: 'var(--text-micro)',
                backgroundColor: 'var(--bg-base)',
                borderColor: 'var(--border-default)',
                borderRadius: 'var(--radius-sharp)',
                color: 'var(--text-primary)',
              }}
            />
          </div>

          <div className="max-h-72 overflow-y-auto p-1">
            <label
              role="option"
              aria-selected={all}
              className="mb-1 flex h-7 cursor-pointer items-center gap-2 border-b px-2 pb-1 transition-colors hover:bg-[var(--bg-hover)]"
              style={{ borderRadius: 'var(--radius-sharp)', borderColor: 'var(--border-subtle)' }}
            >
              <input
                type="checkbox"
                checked={all}
                aria-label="All namespaces"
                onChange={() => onChange([])}
              />
              <span
                className="truncate"
                style={{
                  fontSize: 'var(--text-secondary-size)',
                  color: all ? 'var(--accent)' : 'var(--text-secondary)',
                }}
              >
                All namespaces
              </span>
            </label>

            {visible.map((name) => {
              const checked = isChecked(name)
              return (
                <label
                  key={name}
                  role="option"
                  aria-selected={checked}
                  className="flex h-7 cursor-pointer items-center gap-2 px-2 transition-colors hover:bg-[var(--bg-hover)]"
                  style={{ borderRadius: 'var(--radius-sharp)' }}
                >
                  <input type="checkbox" checked={checked} onChange={() => toggle(name)} />
                  <span
                    className="truncate"
                    style={{
                      fontSize: 'var(--text-secondary-size)',
                      color: checked ? 'var(--text-primary)' : 'var(--text-secondary)',
                    }}
                  >
                    {name}
                  </span>
                </label>
              )
            })}

            {visible.length === 0 && (
              <p className="px-2 py-1.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                No namespace matches.
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
