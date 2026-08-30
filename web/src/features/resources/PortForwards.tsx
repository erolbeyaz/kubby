import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { Callout } from '@/components/Callout'
import { CopyButton } from '@/components/CopyButton'
import { EmptyState } from '@/components/EmptyState'
import { ApiError, api, type Forward } from '@/lib/api'
import { formatAge } from '@/lib/time'

/**
 * Every tunnel this reader has open, in one place.
 *
 * A forward is not a Kubernetes object — it exists only while Kubby holds it — so it has
 * no list of its own anywhere else, and until now the only way to find one was to
 * remember which pod it came from. The columns are the ones that identify it: what it
 * points at inside the cluster, and what to type to reach it from outside.
 */
export function PortForwards({ clusterId }: { clusterId: string }) {
  const queryClient = useQueryClient()
  const [menu, setMenu] = useState<{ id: string; at?: { x: number; y: number } } | null>(null)

  const forwards = useQuery({
    queryKey: ['forwards', clusterId],
    queryFn: ({ signal }) => api.forwards(clusterId, signal),
    // Kubby closes an idle tunnel on its own, so a list nobody refreshes offers to stop
    // something that is already gone.
    refetchInterval: 15_000,
  })

  const error = forwards.error instanceof ApiError ? forwards.error : null
  const rows = forwards.data?.forwards ?? []

  const stop = async (forward: Forward) => {
    setMenu(null)
    try {
      await api.stopForward(forward.id)
    } finally {
      void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })
    }
  }

  const addressOf = (forward: Forward) =>
    forward.mode === 'port' ? forward.url : new URL(forward.url, window.location.origin).toString()

  return (
    <div className="flex h-full min-h-0 flex-col" onClick={() => setMenu(null)}>
      <header
        className="flex h-12 shrink-0 items-center gap-2 border-b px-3"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
          Port forwarding
        </span>
        <span className="font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {rows.length} open
        </span>
      </header>

      {error && (
        <div className="p-4">
          <Callout tone="error" title="Could not read the open forwards" requestId={error.requestId}>
            {error.message}
          </Callout>
        </div>
      )}

      {!error && rows.length === 0 && !forwards.isLoading && (
        <EmptyState
          title="Nothing is forwarded"
          description="Open one from a port on a pod or a service, and it will be listed here until you stop it."
        />
      )}

      {rows.length > 0 && (
        <div className="min-h-0 flex-1 overflow-auto">
          <table className="w-full" style={{ fontSize: 'var(--text-secondary-size)' }}>
            <thead>
              <tr style={{ color: 'var(--text-muted)' }}>
                <Th>Name</Th>
                <Th>Namespace</Th>
                <Th>Kind</Th>
                <Th>Pod port</Th>
                <Th>Local port</Th>
                <Th>Protocol</Th>
                <Th>Age</Th>
                <Th>Status</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {rows.map((forward) => (
                <tr
                  key={forward.id}
                  onContextMenu={(event) => {
                    // Right-clicking a tunnel is the same question as pressing its menu,
                    // asked with the other hand.
                    event.preventDefault()
                    setMenu({ id: forward.id, at: { x: event.clientX, y: event.clientY } })
                  }}
                  className="border-b transition-colors hover:bg-[var(--bg-hover)]"
                  style={{ borderColor: 'var(--border-subtle)' }}
                >
                  <Td>
                    <a
                      href={forward.url}
                      target="_blank"
                      rel="noreferrer"
                      className="font-mono hover:underline"
                      style={{ color: 'var(--status-info)' }}
                    >
                      {forward.name}
                    </a>
                  </Td>
                  <Td mono>{forward.namespace}</Td>
                  <Td>{forward.kind}</Td>
                  <Td mono>{forward.port}</Td>
                  <Td mono>{forward.localPort ?? '—'}</Td>
                  <Td>{forward.protocol}</Td>
                  <Td mono>{formatAge(forward.startedAt)}</Td>
                  <Td>
                    <span style={{ color: 'var(--status-ok)' }}>Active</span>
                    {forward.mode === 'proxy' && (
                      <span className="ml-1.5" style={{ color: 'var(--text-muted)' }} title={forward.note}>
                        proxied
                      </span>
                    )}
                  </Td>
                  <Td>
                    <span className="flex items-center justify-end gap-1.5">
                      <CopyButton value={addressOf(forward)} label="Copy" />
                      <button
                        type="button"
                        onClick={(event) => {
                          event.stopPropagation()
                          setMenu(menu?.id === forward.id ? null : { id: forward.id })
                        }}
                        aria-label={`Actions for ${forward.name}`}
                        className="tool-button"
                      >
                        ⋮
                      </button>
                    </span>

                    {menu?.id === forward.id && (
                      <span
                        className={menu.at ? 'fixed z-30 flex flex-col border shadow-lg' : 'absolute right-4 z-30 mt-1 flex flex-col border shadow-lg'}
                        style={{
                          ...(menu.at ? { left: menu.at.x, top: menu.at.y } : {}),
                          borderRadius: 'var(--radius-sharp)',
                          borderColor: 'var(--border-default)',
                          backgroundColor: 'var(--bg-raised)',
                        }}
                        onClick={(event) => event.stopPropagation()}
                      >
                        <a
                          href={forward.url}
                          target="_blank"
                          rel="noreferrer"
                          className="px-3 py-1.5 text-left transition-colors hover:bg-[var(--bg-hover)]"
                          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
                        >
                          Open
                        </a>
                        <button
                          type="button"
                          onClick={() => void stop(forward)}
                          className="px-3 py-1.5 text-left transition-colors hover:bg-[var(--bg-hover)]"
                          style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}
                        >
                          Stop
                        </button>
                      </span>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function Th({ children }: { children?: React.ReactNode }) {
  return (
    <th
      className="border-b px-3 py-2 text-left font-normal"
      style={{ borderColor: 'var(--border-default)', fontSize: 'var(--text-micro)' }}
    >
      {children}
    </th>
  )
}

function Td({ children, mono }: { children?: React.ReactNode; mono?: boolean }) {
  return (
    <td
      className={`relative px-3 py-1.5 ${mono ? 'font-mono' : ''}`}
      style={{ color: 'var(--text-secondary)' }}
    >
      {children}
    </td>
  )
}
