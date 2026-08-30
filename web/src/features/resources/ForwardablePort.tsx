import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ApiError, api } from '@/lib/api'

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
 * A port, and one click to reach it.
 *
 * The number is where the reader already is when they wonder what is listening on it, so
 * the forward starts here rather than behind a menu and a dialog. The tab opens from the
 * click itself — a browser only treats a new window as wanted while it can still see the
 * gesture that asked for one.
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
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const text = `${port}/${protocol}${label ? ` ${label}` : ''}`
  if (protocol !== 'TCP') {
    return (
      <span className="font-mono" style={{ color: 'var(--text-secondary)' }} title="Only TCP can be forwarded">
        {text}
      </span>
    )
  }

  const forward = async () => {
    setBusy(true)
    setError('')
    try {
      const opened = await api.startForward(clusterId, { type: typeKey, namespace, name, port })
      void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })
      window.open(opened.url, '_blank', 'noopener')
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : 'The cluster refused this.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <span className="inline-flex items-center gap-1">
      <button
        type="button"
        onClick={() => void forward()}
        disabled={busy}
        // The label says what pressing it does; the text on it says which port. A
        // screen reader given only "80/TCP http" is told a fact, not an action.
        aria-label={`Forward ${port} and open it`}
        title={`Forward ${port} and open it`}
        className="tool-chip font-mono"
        style={{ color: 'var(--accent)', borderColor: 'var(--border-default)' }}
      >
        {busy ? 'Opening…' : text}
      </button>
      {error && (
        <span style={{ color: 'var(--status-error)' }} title={error}>
          failed
        </span>
      )}
    </span>
  )
}
