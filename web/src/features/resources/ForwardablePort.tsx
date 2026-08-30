import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ApiError, api, type Forward } from '@/lib/api'

import { PortForwardDialog } from './PortForwardDialog'

interface ForwardablePortProps {
  clusterId: string
  typeKey: string
  namespace: string
  name: string
  port: number
  protocol: string
  label?: string | undefined
}

/**
 * A port, the address it is reachable at, and the button that ends it.
 *
 * Two ways in, because there are two questions. Clicking the address is "just let me see
 * it", which needs no decisions: a free local port and a new tab. The button beside it is
 * for the times a decision is needed — a fixed local port, or a target that speaks TLS
 * where the scheme has to be right or nothing loads.
 *
 * Once a tunnel exists the button ends it, because that is the only thing left to do
 * with it and the reader should not have to go looking for where.
 *
 * Only TCP: a forward carries a stream, and UDP is not one.
 */
export function ForwardablePort({
  clusterId,
  typeKey,
  namespace,
  name,
  port,
  protocol,
  label,
}: ForwardablePortProps) {
  const queryClient = useQueryClient()
  const [configuring, setConfiguring] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Shared with the toolbar chip, so opening one here lights the other up without a
  // second request.
  const forwards = useQuery({
    queryKey: ['forwards', clusterId],
    queryFn: ({ signal }) => api.forwards(clusterId, signal),
    staleTime: 5_000,
  })

  const open = (forwards.data?.forwards ?? []).find(
    (forward) => forward.namespace === namespace && forward.port === port && forward.name === name,
  )

  const text = `${port}/${protocol}${label ? ` ${label}` : ''}`
  if (protocol !== 'TCP') {
    return (
      <span className="font-mono" style={{ color: 'var(--text-secondary)' }} title="Only TCP can be forwarded">
        {text}
      </span>
    )
  }

  const openNow = async () => {
    if (open) {
      window.open(open.url, '_blank', 'noopener')
      return
    }
    setBusy(true)
    setError('')
    try {
      const started = await api.startForward(clusterId, { type: typeKey, namespace, name, port })
      void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })
      window.open(started.url, '_blank', 'noopener')
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : 'The cluster refused this.')
    } finally {
      setBusy(false)
    }
  }

  const stop = async (forward: Forward) => {
    setBusy(true)
    try {
      await api.stopForward(forward.id)
    } finally {
      void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })
      setBusy(false)
    }
  }

  return (
    <>
      {/* The address on the left and the action on the right, one port to a line. Read
          down the column, act at the edge — a row of chips side by side made the two
          jobs compete for the same glance. */}
      <span className="flex w-full items-center gap-3">
        <button
          type="button"
          onClick={() => void openNow()}
          disabled={busy}
          aria-label={open ? `Open ${port} in a new tab` : `Forward ${port} and open it`}
          title={open ? open.url : `Forward ${port} and open it`}
          className="min-w-0 flex-1 truncate text-left font-mono transition-colors hover:underline"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--status-info)' }}
        >
          {busy ? 'Opening…' : text}
        </button>

        {error && (
          <span style={{ color: 'var(--status-error)' }} title={error}>
            failed
          </span>
        )}

        <button
          type="button"
          onClick={() => (open ? void stop(open) : setConfiguring(true))}
          disabled={busy}
          className="shrink-0 px-2.5 py-1 font-semibold uppercase tracking-[0.06em] transition-colors"
          // The same shape either way, so the pair reads as one control changing state
          // rather than two buttons that happen to share a place.
          style={{
            borderRadius: 'var(--radius-sharp)',
            fontSize: 'var(--text-micro)',
            backgroundColor: open ? 'var(--status-error)' : 'var(--accent)',
            color: 'var(--text-inverse)',
          }}
        >
          {open ? 'Stop' : 'Forward…'}
        </button>
      </span>

      {configuring && (
        <PortForwardDialog
          clusterId={clusterId}
          typeKey={typeKey}
          name={name}
          namespace={namespace}
          port={port}
          onOpened={() => void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })}
          onClose={() => setConfiguring(false)}
        />
      )}
    </>
  )
}
