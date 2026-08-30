import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { CopyButton } from '@/components/CopyButton'
import { api, type Forward } from '@/lib/api'

interface ForwardPaneProps {
  forward: Forward
  onClosed: () => void
}

/**
 * An open tunnel, and what to do with it.
 *
 * A forward on a real local port is not shown inside Kubby: the point of giving the
 * application a port of its own is that it gets an origin of its own, and framing it here
 * would only put a working page back inside a window it does not need. So this is the
 * address, the state, and the button that ends it — the tunnel stays open until it is
 * stopped, or until nothing has used it for a while.
 *
 * A proxied forward is still framed, because there is no address to hand out. That path
 * cannot serve every application (its absolute asset paths resolve to Kubby, and a
 * single-page app builds URLs where no rewrite reaches them), which is why the panel says
 * which of the two this is.
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

  const local = forward.mode === 'port'
  const address = local ? forward.url : new URL(forward.url, window.location.origin).toString()

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
        <span style={{ color: 'var(--text-muted)' }}>→</span>
        <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
          {address}
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
          <CopyButton value={address} label="Copy link" />
          <button type="button" onClick={() => void stop()} className="tool-button" disabled={stopped}>
            {stopped ? 'Stopped' : 'Stop forwarding'}
          </button>
        </span>
      </div>

      {stopped ? (
        <Message>This tunnel is closed.</Message>
      ) : local ? (
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
            Listening on{' '}
            <span className="font-mono" style={{ color: 'var(--accent)' }}>
              {address}
            </span>
          </p>
          <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)', maxWidth: '46ch' }}>
            The port is open on the machine running Kubby and stays open until you stop it,
            or until nothing has used it for a while. It carries no authentication of its
            own, so it is bound to loopback unless this deployment was configured otherwise.
          </p>
        </div>
      ) : (
        <>
          {forward.note && (
            <p
              className="shrink-0 border-b px-3 py-1.5"
              style={{
                borderColor: 'var(--border-subtle)',
                fontSize: 'var(--text-micro)',
                color: 'var(--status-warn)',
              }}
            >
              {forward.note}
            </p>
          )}
          <iframe
            title={`${forward.pod}:${forward.port}`}
            src={forward.url}
            className="min-h-0 flex-1 border-0"
            style={{ backgroundColor: 'var(--bg-base)' }}
          />
        </>
      )}
    </div>
  )
}

function Message({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="flex flex-1 items-center justify-center"
      style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
    >
      {children}
    </div>
  )
}
