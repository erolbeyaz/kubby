import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { environmentColor } from '@/features/clusters/environment'
import { api, type Environment, type SearchHit } from '@/lib/api'

interface CommandPaletteProps {
  onClose: () => void
  onOpen: (hit: SearchHit) => void
}

/**
 * One box, every cluster.
 *
 * The question it answers is "where is this thing", which in a fleet is otherwise a
 * cluster picker, a kind, a namespace filter and a search box — four decisions before the
 * first character is typed. Here the answer comes back with the cluster attached.
 *
 * Keyboard first throughout: this is opened with Ctrl+K by someone who has already
 * decided what they are looking for, and reaching for the mouse to pick a result would
 * undo the point of it.
 */
export function CommandPalette({ onClose, onOpen }: CommandPaletteProps) {
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const field = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLUListElement>(null)

  // Debounced: every keystroke fans out to a list call on every API server in the fleet,
  // so typing "payments" must not be eight rounds of that.
  const [settled, setSettled] = useState('')
  useEffect(() => {
    const timer = setTimeout(() => setSettled(query.trim()), 180)
    return () => clearTimeout(timer)
  }, [query])

  const results = useQuery({
    queryKey: ['search', settled],
    queryFn: ({ signal }) => api.search(settled, signal),
    enabled: settled.length >= 2,
    // A stale hit is worse than none: it sends the reader to an object that has gone.
    staleTime: 0,
  })

  const hits = results.data?.hits ?? []
  // Clamped rather than reset: results shrink as the query narrows, and an index past the
  // end would leave Enter opening nothing.
  const cursor = Math.min(selected, Math.max(hits.length - 1, 0))
  const unreachable = results.data?.unreachable ?? []

  useEffect(() => {
    field.current?.focus()
  }, [])

  // Keeps the highlighted row in view when it is moved with the keyboard.
  useEffect(() => {
    list.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  const onKeyDown = (event: React.KeyboardEvent) => {
    switch (event.key) {
      case 'Escape':
        event.preventDefault()
        onClose()
        break
      case 'ArrowDown':
        event.preventDefault()
        setSelected(Math.min(cursor + 1, hits.length - 1))
        break
      case 'ArrowUp':
        event.preventDefault()
        setSelected(Math.max(cursor - 1, 0))
        break
      case 'Enter': {
        event.preventDefault()
        const hit = hits[cursor]
        if (hit) onOpen(hit)
        break
      }
    }
  }

  return (
    <div
      className="fixed inset-0 z-[70] flex items-start justify-center p-4 pt-[12vh]"
      style={{ backgroundColor: 'color-mix(in srgb, var(--bg-base) 72%, transparent)' }}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div
        role="dialog"
        aria-label="Search everything"
        className="flex max-h-[70vh] w-full max-w-2xl flex-col overflow-hidden border shadow-2xl"
        style={{
          borderColor: 'var(--border-strong)',
          backgroundColor: 'var(--bg-overlay)',
          borderRadius: 'var(--radius-panel)',
        }}
      >
        <div
          className="flex items-center gap-2 border-b px-3"
          style={{ borderColor: 'var(--border-subtle)' }}
        >
          <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true" className="shrink-0 opacity-60">
            <circle cx="7" cy="7" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.4" />
            <path d="M10.5 10.5 L14 14" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
          </svg>

          <input
            ref={field}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value)
              // Reset here rather than when the results arrive: typing is the event that
              // invalidates the choice, and moving it into an effect makes the highlight
              // jump under the reader's hands a moment after they stopped typing.
              setSelected(0)
            }}
            onKeyDown={onKeyDown}
            placeholder="Search every cluster…"
            aria-label="Search every cluster"
            className="h-11 min-w-0 flex-1 bg-transparent outline-none"
            style={{ fontSize: 'var(--text-body)', color: 'var(--text-primary)' }}
          />

          {results.isFetching && (
            <span
              aria-hidden="true"
              className="h-3 w-3 shrink-0 animate-spin rounded-full border border-current border-t-transparent"
              style={{ color: 'var(--text-muted)' }}
            />
          )}
          <span className="kbd shrink-0">Esc</span>
        </div>

        <ul ref={list} className="min-h-0 flex-1 overflow-y-auto">
          {hits.map((hit, index) => (
            <li key={`${hit.clusterId}/${hit.typeKey}/${hit.namespace ?? ''}/${hit.name}`}>
              <button
                type="button"
                data-selected={index === cursor}
                onMouseEnter={() => setSelected(index)}
                onClick={() => onOpen(hit)}
                className="flex w-full items-center gap-2.5 px-3 py-1.5 text-left"
                style={{
                  backgroundColor: index === cursor ? 'var(--bg-active)' : undefined,
                  boxShadow: index === cursor ? 'inset 2px 0 0 0 var(--accent)' : undefined,
                }}
              >
                {/* The cluster's own colour, because in a fleet the first thing worth
                    knowing about a result is which cluster it is in. */}
                <span
                  aria-hidden="true"
                  className="h-5 w-1 shrink-0"
                  style={{ backgroundColor: environmentColor(hit.environment as Environment, '') }}
                />

                <span
                  className="w-24 shrink-0 truncate font-mono"
                  style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
                  title={hit.clusterName}
                >
                  {hit.clusterName}
                </span>

                <span
                  className="w-20 shrink-0 truncate"
                  style={{ fontSize: 'var(--text-micro)', color: 'var(--status-info)' }}
                >
                  {hit.kind}
                </span>

                <span
                  className="min-w-0 flex-1 truncate"
                  style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}
                >
                  {hit.namespace ? (
                    <span style={{ color: 'var(--text-muted)' }}>{hit.namespace} / </span>
                  ) : null}
                  {hit.name}
                </span>

                {hit.status && (
                  <span
                    className="shrink-0 font-mono"
                    style={{
                      fontSize: 'var(--text-micro)',
                      color:
                        hit.severity === 'error'
                          ? 'var(--status-error)'
                          : hit.severity === 'warn'
                            ? 'var(--status-warn)'
                            : 'var(--text-muted)',
                    }}
                  >
                    {hit.status}
                  </span>
                )}
              </button>
            </li>
          ))}

          {settled.length >= 2 && !results.isFetching && hits.length === 0 && (
            <li
              className="px-3 py-6 text-center"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            >
              Nothing matched “{settled}”.
            </li>
          )}

          {settled.length < 2 && (
            <li
              className="px-3 py-6 text-center"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            >
              Type at least two characters. Pods, workloads, services, namespaces, nodes,
              config — across every cluster you can see.
            </li>
          )}
        </ul>

        {/* A search that quietly returns fewer results because a cluster is down is a
            search that lies: the reader concludes the object does not exist. */}
        {unreachable.length > 0 && (
          <div
            className="shrink-0 border-t px-3 py-1.5"
            style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
          >
            <span style={{ color: 'var(--status-warn)' }}>
              Not searched: {unreachable.map((problem) => problem.clusterName).join(', ')}
            </span>
            <span style={{ color: 'var(--text-muted)' }}> — results may be incomplete.</span>
          </div>
        )}

        <div
          className="flex shrink-0 items-center gap-3 border-t px-3 py-1.5"
          style={{ borderColor: 'var(--border-subtle)', fontSize: '10px', color: 'var(--text-muted)' }}
        >
          <span className="flex items-center gap-1">
            <span className="kbd">↑</span>
            <span className="kbd">↓</span>
            move
          </span>
          <span className="flex items-center gap-1">
            <span className="kbd">↵</span>
            open
          </span>
          {results.data?.truncated && <span className="ml-auto">showing the closest matches</span>}
        </div>
      </div>
    </div>
  )
}
