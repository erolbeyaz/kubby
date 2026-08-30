import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { CopyButton } from '@/components/CopyButton'
import { ApiError, api, type Forward, type ResourceRow } from '@/lib/api'

interface PortForwardDialogProps {
  clusterId: string
  typeKey: string
  kind: string
  row: ResourceRow
  onOpened: (forward: Forward) => void
  onClose: () => void
}

/**
 * Reaching a port inside the cluster.
 *
 * Kubby opens a real port on its own host and pipes it to the pod, which is what makes an
 * arbitrary web application work: it gets an origin of its own at its own root, so its
 * absolute asset paths and its cookies both land where it expects them. Serving it under
 * a path of Kubby's instead breaks every app that builds a URL in JavaScript.
 *
 * That port is on the machine running Kubby. When that is the reader's own machine — the
 * ordinary case — it is theirs to open. When it is not, the tunnel falls back to the
 * proxy and the dialog says so rather than handing out an address nobody can reach.
 */
export function PortForwardDialog({
  clusterId,
  typeKey,
  kind,
  row,
  onOpened,
  onClose,
}: PortForwardDialogProps) {
  const queryClient = useQueryClient()
  const namespace = row.namespace ?? ''

  const ports = useQuery({
    queryKey: ['ports', clusterId, typeKey, namespace, row.name],
    queryFn: ({ signal }) => api.forwardablePorts(clusterId, typeKey, namespace, row.name, signal),
  })

  const declared = ports.data?.ports ?? []
  const [chosen, setChosen] = useState<number | null>(null)
  const [typed, setTyped] = useState('')
  const [localPort, setLocalPort] = useState('')
  const [openInBrowser, setOpenInBrowser] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // A declared port is the common case; a typed one covers a container that listens on
  // more than it declares, which is normal and not worth blocking on.
  const port = typed.trim() !== '' ? Number(typed) : (chosen ?? declared[0]?.port ?? null)
  const valid = port !== null && Number.isInteger(port) && port > 0 && port <= 65535

  const run = async () => {
    if (!valid) return
    setBusy(true)
    setError('')
    try {
      const forward = await api.startForward(clusterId, {
        type: typeKey,
        namespace,
        name: row.name,
        port,
        ...(localPort.trim() ? { localPort: Number(localPort) } : {}),
      })
      void queryClient.invalidateQueries({ queryKey: ['forwards', clusterId] })

      // Opened here rather than after the dialog closes: a browser only treats a new
      // window as wanted while it can still see the click that asked for it.
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
      title={`Forward a port from ${kind} ${namespace ? `${namespace}/` : ''}${row.name}`}
      confirmLabel="Start"
      busy={busy}
      disabled={!valid}
      {...(error ? { error } : {})}
      onConfirm={() => void run()}
      onCancel={onClose}
    >
      {ports.isPending ? (
        <p style={{ color: 'var(--text-muted)' }}>Reading the declared ports…</p>
      ) : declared.length === 0 ? (
        <p style={{ color: 'var(--text-muted)' }}>
          This {kind.toLowerCase()} declares no ports. Type one below if you know what it
          listens on.
        </p>
      ) : (
        <div className="flex flex-wrap justify-center gap-1.5">
          {declared.map((option) => {
            const selected = typed.trim() === '' && (chosen ?? declared[0]?.port) === option.port
            return (
              <button
                key={`${option.container ?? ''}:${option.port}`}
                type="button"
                onClick={() => {
                  setChosen(option.port)
                  setTyped('')
                }}
                className="tool-chip font-mono"
                style={{
                  borderColor: selected ? 'var(--accent)' : 'var(--border-default)',
                  color: selected ? 'var(--accent)' : 'var(--text-secondary)',
                }}
              >
                {option.port}
                {option.name ? ` · ${option.name}` : ''}
                {option.protocol !== 'TCP' ? ` · ${option.protocol}` : ''}
              </button>
            )
          })}
        </div>
      )}

      <label
        className="mt-3 flex items-center justify-center gap-2"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        or port
        <input
          value={typed}
          onChange={(event) => setTyped(event.target.value.replace(/[^0-9]/g, ''))}
          inputMode="numeric"
          placeholder="8080"
          className="w-24 border px-2 py-1 text-center font-mono"
          style={{
            borderRadius: 'var(--radius-sharp)',
            borderColor: 'var(--border-default)',
            backgroundColor: 'var(--bg-raised)',
            color: 'var(--text-primary)',
            fontSize: 'var(--text-secondary-size)',
          }}
        />
      </label>

      <label
        className="mt-3 flex items-center justify-center gap-2"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        local port
        <input
          value={localPort}
          onChange={(event) => setLocalPort(event.target.value.replace(/[^0-9]/g, ''))}
          inputMode="numeric"
          placeholder="Random"
          aria-label="Local port to forward from"
          className="w-24 border px-2 py-1 text-center font-mono"
          style={{
            borderRadius: 'var(--radius-sharp)',
            borderColor: 'var(--border-default)',
            backgroundColor: 'var(--bg-raised)',
            color: 'var(--text-primary)',
            fontSize: 'var(--text-secondary-size)',
          }}
        />
      </label>

      <label
        className="mt-2 flex items-center justify-center gap-2"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
      >
        <input
          type="checkbox"
          checked={openInBrowser}
          onChange={(event) => setOpenInBrowser(event.target.checked)}
        />
        Open in a new browser tab
      </label>

      <p className="mt-3" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        For a port that does not speak HTTP — a database, gRPC — run it locally instead:
      </p>

      <div className="mt-1.5 flex items-center justify-center gap-2">
        <code
          className="truncate px-2 py-1 font-mono"
          style={{
            fontSize: 'var(--text-micro)',
            backgroundColor: 'var(--bg-raised)',
            color: 'var(--text-secondary)',
            borderRadius: 'var(--radius-sharp)',
          }}
        >
          {kubectlCommand(namespace, typeKey, row.name, port)}
        </code>
        <CopyButton value={kubectlCommand(namespace, typeKey, row.name, port)} label="Copy" />
      </div>
    </ConfirmDialog>
  )
}

/** The same forward as a local one, for a port a browser cannot speak to. */
function kubectlCommand(namespace: string, typeKey: string, name: string, port: number | null): string {
  const kind = typeKey.includes('/') ? typeKey.split('/')[1] : typeKey
  const singular = kind?.replace(/s$/, '') ?? 'pod'
  const number = port ?? 8080
  return `kubectl -n ${namespace} port-forward ${singular}/${name} ${number}:${number}`
}
