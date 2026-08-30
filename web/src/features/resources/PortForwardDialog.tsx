import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ApiError, api, type Forward } from '@/lib/api'

interface PortForwardDialogProps {
  clusterId: string
  typeKey: string
  /** What is being forwarded, for the title. */
  name: string
  namespace: string
  /**
   * The port inside the cluster. Absent when the dialog was reached from the row's own
   * menu rather than from a port, in which case it asks which one.
   */
  port?: number | undefined
  onOpened: (forward: Forward) => void
  onClose: () => void
}

/**
 * Where a forward is configured, for the times the defaults are not right.
 *
 * Reaching a port usually needs no decisions at all — that is what clicking the port
 * itself is for. This is the other case: a fixed local port because something is
 * configured to expect one, or a target that speaks TLS, where the scheme has to be
 * right or the page will not load.
 */
export function PortForwardDialog({
  clusterId,
  typeKey,
  name,
  namespace,
  port,
  onOpened,
  onClose,
}: PortForwardDialogProps) {
  const queryClient = useQueryClient()
  const [chosen, setChosen] = useState<number | null>(port ?? null)
  const [localPort, setLocalPort] = useState('')
  const [https, setHttps] = useState(false)
  const [openInBrowser, setOpenInBrowser] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Only asked for when the caller did not already know: clicking a port answers this
  // question by existing.
  const declared = useQuery({
    queryKey: ['ports', clusterId, typeKey, namespace, name],
    queryFn: ({ signal }) => api.forwardablePorts(clusterId, typeKey, namespace, name, signal),
    enabled: port === undefined,
  })

  const ports = declared.data?.ports ?? []
  const target = chosen ?? ports[0]?.port ?? null

  const start = async () => {
    if (target === null) return
    setBusy(true)
    setError('')
    try {
      const forward = await api.startForward(clusterId, {
        type: typeKey,
        namespace,
        name,
        port: target,
        ...(localPort.trim() ? { localPort: Number(localPort) } : {}),
        ...(https ? { https: true } : {}),
      })
      void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })

      // Opened from the click that asked for it: a browser only counts a new window as
      // wanted while it can still see the gesture behind it.
      if (openInBrowser) {
        window.open(forward.url, '_blank', 'noopener')
      }
      onOpened(forward)
      onClose()
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : 'The cluster refused this.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <ConfirmDialog
      title={`Port forwarding for ${name}`}
      confirmLabel="Start"
      busy={busy}
      disabled={target === null}
      {...(error ? { error } : {})}
      onConfirm={() => void start()}
      onCancel={onClose}
    >
      <div className="flex flex-col gap-3 text-left">
        {port === undefined && (
          <div className="flex flex-col gap-1.5">
            <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}>
              Port to forward:
            </span>
            {ports.length === 0 ? (
              <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                {declared.isPending
                  ? 'Reading the declared ports…'
                  : 'Nothing is declared here. Forward from a port on the detail panel instead.'}
              </span>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {ports.map((option) => (
                  <button
                    key={`${option.container ?? ''}:${option.port}`}
                    type="button"
                    onClick={() => setChosen(option.port)}
                    className="tool-chip font-mono"
                    style={{
                      borderColor: target === option.port ? 'var(--accent)' : 'var(--border-default)',
                      color: target === option.port ? 'var(--accent)' : 'var(--text-secondary)',
                    }}
                  >
                    {option.port}
                    {option.name ? ` · ${option.name}` : ''}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

        <label className="flex items-center gap-3" style={{ fontSize: 'var(--text-secondary-size)' }}>
          <span style={{ color: 'var(--text-secondary)' }}>Local port to forward from:</span>
          <input
            value={localPort}
            onChange={(event) => setLocalPort(event.target.value.replace(/[^0-9]/g, ''))}
            inputMode="numeric"
            placeholder="Random"
            aria-label="Local port to forward from"
            autoFocus
            className="min-w-0 flex-1 border-0 border-b bg-transparent px-1 py-1 font-mono outline-none"
            style={{
              borderColor: 'var(--accent)',
              color: 'var(--text-primary)',
              fontSize: 'var(--text-secondary-size)',
            }}
          />
        </label>

        <label className="flex items-center gap-2" style={{ fontSize: 'var(--text-secondary-size)' }}>
          <input type="checkbox" checked={https} onChange={(event) => setHttps(event.target.checked)} />
          {/* The tunnel carries bytes either way; this decides the scheme of the address
              handed to the browser, and the wrong one is a page that will not load. */}
          <span style={{ color: 'var(--text-secondary)' }}>https</span>
        </label>

        <label className="flex items-center gap-2" style={{ fontSize: 'var(--text-secondary-size)' }}>
          <input
            type="checkbox"
            checked={openInBrowser}
            onChange={(event) => setOpenInBrowser(event.target.checked)}
          />
          <span style={{ color: 'var(--text-secondary)' }}>Open in Browser</span>
        </label>

        <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          Forwarding {namespace ? `${namespace}/` : ''}
          {name}
          {target !== null ? `:${target}` : ''}. The port opens on the machine running Kubby
          and carries no authentication of its own.
        </p>
      </div>
    </ConfirmDialog>
  )
}
