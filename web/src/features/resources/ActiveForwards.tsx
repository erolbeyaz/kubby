import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { CopyButton } from '@/components/CopyButton'
import { api, type Forward } from '@/lib/api'

/**
 * The tunnels that are open, and the button that ends one.
 *
 * A chip rather than a panel. A forward on a port of its own is opened in a browser tab
 * and lived in there; the only thing left to do inside Kubby is stop it, and giving that
 * a whole pane spent the screen on a list that is usually empty and never read.
 *
 * Absent entirely while nothing is forwarded, so it costs nothing when it has nothing
 * to say.
 */
export function ActiveForwards({ clusterId }: { clusterId: string }) {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)

  const forwards = useQuery({
    queryKey: ['forwards', clusterId],
    queryFn: ({ signal }) => api.forwards(clusterId, signal),
    // The server closes an idle tunnel on its own, so a list nobody refreshes goes stale
    // and offers to stop something that is already gone.
    refetchInterval: 30_000,
  })

  const list = forwards.data?.forwards ?? []
  if (list.length === 0) return null

  const stop = async (forward: Forward) => {
    try {
      await api.stopForward(forward.id)
    } finally {
      void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })
    }
  }

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
        className="tool-chip flex items-center gap-1.5"
        style={{ borderColor: 'var(--accent)', color: 'var(--accent)' }}
      >
        <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--status-ok)' }} />
        {list.length} forward{list.length === 1 ? '' : 's'}
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Open port forwards"
          className="absolute right-0 z-40 mt-1 flex w-[26rem] flex-col border shadow-lg"
          style={{
            borderRadius: 'var(--radius-sharp)',
            borderColor: 'var(--border-default)',
            backgroundColor: 'var(--bg-raised)',
          }}
        >
          {list.map((forward) => (
            <div
              key={forward.id}
              className="flex flex-col gap-1 border-b px-2.5 py-2 last:border-b-0"
              style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
            >
              <span className="truncate font-mono" style={{ color: 'var(--text-secondary)' }}>
                {forward.namespace}/{forward.pod}:{forward.port}
              </span>

              <div className="flex items-center gap-2">
                <a
                  href={forward.url}
                  target="_blank"
                  rel="noreferrer"
                  className="min-w-0 flex-1 truncate font-mono hover:underline"
                  style={{ color: 'var(--accent)' }}
                >
                  {forward.mode === 'port' ? forward.url : new URL(forward.url, window.location.origin).toString()}
                </a>
                <CopyButton
                  value={
                    forward.mode === 'port'
                      ? forward.url
                      : new URL(forward.url, window.location.origin).toString()
                  }
                  label="Copy"
                />
                <button type="button" onClick={() => void stop(forward)} className="tool-button">
                  Stop
                </button>
              </div>

              {forward.note && (
                <span style={{ color: 'var(--status-warn)' }}>{forward.note}</span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
