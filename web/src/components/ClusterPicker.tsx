import { useEffect, useRef, useState } from 'react'

import { environmentColor } from '@/features/clusters/environment'
import type { Cluster } from '@/lib/api'

interface ClusterPickerProps {
  clusters: Cluster[]
  current: Cluster | null
  onSelect: (clusterId: string) => void
  onManage: () => void
  canManage: boolean
}

const STATUS_COLOR: Record<string, string> = {
  valid: 'var(--status-ok)',
  invalid: 'var(--status-error)',
  unreachable: 'var(--status-warn)',
  unknown: 'var(--status-unknown)',
}

/**
 * Which cluster everything else is about.
 *
 * It sits top-left because it governs the whole window: the navigation, the lists and
 * the detail are all "within this cluster". Reaching it through a management screen
 * made switching feel like an administrative act rather than the routine move it is.
 */
export function ClusterPicker({ clusters, current, onSelect, onManage, canManage }: ClusterPickerProps) {
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
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label="Select cluster"
        onClick={() => setOpen((value) => !value)}
        className="flex h-8 items-center gap-2 border px-2.5 transition-colors"
        style={{
          minWidth: '11rem',
          borderRadius: 'var(--radius-sharp)',
          borderColor: open ? 'var(--accent)' : 'var(--border-default)',
          backgroundColor: 'var(--bg-raised)',
          fontSize: 'var(--text-secondary-size)',
          color: 'var(--text-primary)',
        }}
      >
        {current ? (
          <>
            <span
              aria-hidden="true"
              className="h-4 w-1 shrink-0"
              style={{ backgroundColor: environmentColor(current.environment, current.color) }}
            />
            <span className="flex-1 truncate text-left">{current.name}</span>
            <span
              aria-hidden="true"
              className="h-1.5 w-1.5 shrink-0 rounded-full"
              style={{ backgroundColor: STATUS_COLOR[current.credentialStatus] }}
            />
          </>
        ) : (
          <span className="flex-1 text-left" style={{ color: 'var(--text-muted)' }}>
            No cluster
          </span>
        )}
        <svg width="8" height="8" viewBox="0 0 10 10" aria-hidden="true" className="shrink-0 opacity-60">
          <path d="M2 3.5 L5 6.5 L8 3.5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      </button>

      {open && (
        <div
          role="listbox"
          className="absolute left-0 z-50 mt-1 w-72 border shadow-lg"
          style={{
            backgroundColor: 'var(--bg-overlay)',
            borderColor: 'var(--border-strong)',
            borderRadius: 'var(--radius-panel)',
          }}
        >
          <div className="max-h-80 overflow-y-auto p-1">
            {clusters.length === 0 && (
              <p className="px-2.5 py-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                No clusters yet.
              </p>
            )}

            {clusters.map((cluster) => (
              <button
                key={cluster.id}
                role="option"
                aria-selected={cluster.id === current?.id}
                type="button"
                onClick={() => {
                  onSelect(cluster.id)
                  setOpen(false)
                }}
                className="flex w-full items-center gap-2.5 px-2.5 py-1.5 text-left transition-colors hover:bg-[var(--bg-hover)]"
                style={{
                  borderRadius: 'var(--radius-sharp)',
                  backgroundColor: cluster.id === current?.id ? 'var(--bg-active)' : 'transparent',
                }}
              >
                <span
                  aria-hidden="true"
                  className="h-6 w-1 shrink-0"
                  style={{ backgroundColor: environmentColor(cluster.environment, cluster.color) }}
                />
                <span className="min-w-0 flex-1">
                  <span
                    className="block truncate"
                    style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}
                  >
                    {cluster.name}
                  </span>
                  <span
                    className="block truncate font-mono"
                    style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
                  >
                    {cluster.displayEnvironment}
                    {cluster.k8sVersion ? ` · ${cluster.k8sVersion}` : ''}
                  </span>
                </span>
                <span
                  aria-hidden="true"
                  className="h-1.5 w-1.5 shrink-0 rounded-full"
                  style={{ backgroundColor: STATUS_COLOR[cluster.credentialStatus] }}
                />
              </button>
            ))}
          </div>

          {canManage && (
            <div className="border-t p-1" style={{ borderColor: 'var(--border-subtle)' }}>
              <button
                type="button"
                onClick={() => {
                  onManage()
                  setOpen(false)
                }}
                className="w-full px-2.5 py-1.5 text-left transition-colors hover:bg-[var(--bg-hover)]"
                style={{
                  borderRadius: 'var(--radius-sharp)',
                  fontSize: 'var(--text-secondary-size)',
                  color: 'var(--text-secondary)',
                }}
              >
                Manage clusters…
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
