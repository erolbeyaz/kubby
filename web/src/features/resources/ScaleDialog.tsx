import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ApiError, api, type ResourceRow } from '@/lib/api'

interface ScaleDialogProps {
  clusterId: string
  typeKey: string
  kind: string
  row: ResourceRow
  onChanged: () => void
  onClose: () => void
}

/**
 * How many replicas a workload should run.
 *
 * A slider rather than a number field: the useful range is small and bounded, and the
 * question is nearly always "a bit more" or "a bit less" rather than a specific figure.
 * The round buttons either side cover the by-one case that a slider is clumsy at.
 */
export function ScaleDialog({ clusterId, typeKey, kind, row, onChanged, onClose }: ScaleDialogProps) {
  const queryClient = useQueryClient()
  const current = currentReplicas(row)

  const [wanted, setWanted] = useState(current)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Fifty covers what anyone reaches for with a slider, and a workload already larger
  // than that can still be doubled rather than being capped below where it started.
  const max = Math.max(50, current * 2)

  const run = async () => {
    setBusy(true)
    setError('')
    try {
      await api.scale(clusterId, {
        typeKey,
        namespace: row.namespace ?? '',
        name: row.name,
        replicas: wanted,
      })
      void queryClient.invalidateQueries({ queryKey: ['resources'] })
      onChanged()
      onClose()
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : 'The cluster refused this.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <ConfirmDialog
      title={`Scale ${kind} ${row.namespace ? `${row.namespace}/` : ''}${row.name}`}
      confirmLabel="Scale"
      busy={busy}
      // Nothing to do when the answer is the number it already is.
      disabled={wanted === current}
      {...(error ? { error } : {})}
      onConfirm={() => void run()}
      onCancel={onClose}
    >
      <p style={{ color: 'var(--text-primary)' }}>
        <strong>Current replica scale: {current}</strong>
      </p>

      <p className="mt-2">
        Desired number of replicas: <span style={{ color: 'var(--text-primary)' }}>{wanted}</span>
      </p>

      <div className="mt-3 flex items-center gap-3">
        <RoundButton
          label="One fewer"
          disabled={wanted <= 0}
          onClick={() => setWanted((value) => Math.max(0, value - 1))}
          path="M4.5 8h7"
        />

        <input
          type="range"
          min={0}
          max={max}
          value={wanted}
          aria-label="Desired number of replicas"
          onChange={(event) => setWanted(Number(event.target.value))}
          className="scale-slider min-w-0 flex-1"
        />

        <RoundButton
          label="One more"
          disabled={wanted >= max}
          onClick={() => setWanted((value) => Math.min(max, value + 1))}
          path="M8 4.5v7M4.5 8h7"
        />
      </div>

      {wanted === 0 && current > 0 && (
        <p className="mt-3" style={{ color: 'var(--status-warn)' }}>
          Zero stops every replica. The object stays, so it can be scaled back up.
        </p>
      )}
    </ConfirmDialog>
  )
}

/** "3/5" in the ready column: the wanted count is the half after the slash. */
function currentReplicas(row: ResourceRow): number {
  const fromReady = Number((row.fields['ready'] ?? '').split('/')[1])
  if (Number.isFinite(fromReady)) return fromReady

  const desired = Number(row.fields['desired'] ?? '0')
  return Number.isFinite(desired) ? desired : 0
}

function RoundButton({
  label,
  path,
  onClick,
  disabled,
}: {
  label: string
  path: string
  onClick: () => void
  disabled: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border transition-colors hover:border-[var(--accent)] hover:text-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-40"
      style={{ borderColor: 'var(--border-strong)', color: 'var(--text-secondary)' }}
    >
      <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
        <path d={path} />
      </svg>
    </button>
  )
}
