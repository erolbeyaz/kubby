import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { CopyButton } from '@/components/CopyButton'
import { api, type Forward } from '@/lib/api'

interface ForwardPaneProps {
  forward: Forward
  onClosed: () => void
}

/**
 * An open tunnel, and the pod's own pages inside it.
 *
 * The frame is sandboxed into an origin of its own by the server, so the page can run but
 * cannot touch Kubby. An app that needs a login of its own will not hold a session in
 * there — that is what the kubectl command below is for.
 */
export function ForwardPane({ forward, onClosed }: ForwardPaneProps) {
  const queryClient = useQueryClient()
  const [stopped, setStopped] = useState(false)

  const stop = async () => {
    setStopped(true)
    try {
      await api.stopForward(forward.id)
    } finally {
      void queryClient.invalidateQueries({ queryKey: ['forwards', forward.clusterId] })
      onClosed()
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-1.5"
        style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
      >
        <span
          aria-hidden="true"
          className="h-1.5 w-1.5 rounded-full"
          style={{ backgroundColor: stopped ? 'var(--status-unknown)' : 'var(--status-ok)' }}
        />
        <span className="font-mono" style={{ color: 'var(--text-secondary)' }}>
          {forward.namespace}/{forward.pod}:{forward.port}
        </span>

        <span className="ml-auto flex items-center gap-2">
          <a
            href={forward.url}
            target="_blank"
            rel="noreferrer"
            className="tool-button"
            style={{ color: 'var(--accent)' }}
          >
            Open in a new tab
          </a>
          <CopyButton value={new URL(forward.url, window.location.origin).toString()} label="Copy link" />
          <button type="button" onClick={() => void stop()} className="tool-button" disabled={stopped}>
            {stopped ? 'Closed' : 'Close tunnel'}
          </button>
        </span>
      </div>

      {stopped ? (
        <div
          className="flex flex-1 items-center justify-center"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
        >
          This tunnel is closed.
        </div>
      ) : (
        <iframe
          title={`${forward.pod}:${forward.port}`}
          src={forward.url}
          className="min-h-0 flex-1 border-0"
          style={{ backgroundColor: 'var(--bg-base)' }}
        />
      )}
    </div>
  )
}
