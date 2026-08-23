import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ApiError, api, type ResourceRow } from '@/lib/api'

export interface DeleteTarget {
  clusterId: string
  typeKey: string
  kind: string
  rows: ResourceRow[]
}

interface DeleteDialogProps {
  target: DeleteTarget
  /** Called once something was removed, so the list can watch what the cluster does. */
  onChanged: () => void
  onClose: () => void
}

interface Failure {
  name: string
  reason: string
}

/**
 * Confirms and performs a delete, one object or many.
 *
 * The dialog lists what will go by name rather than by count: "3 objects" is a number
 * someone can agree to without reading, and the whole point of the gate is that they
 * read it.
 */
export function DeleteDialog({ target, onChanged, onClose }: DeleteDialogProps) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [failures, setFailures] = useState<Failure[]>([])

  const { rows, kind } = target
  const single = rows.length === 1

  // A pod belonging to a controller comes back within a second, which reads as the
  // delete having failed. Saying so first turns a confusing non-event into a decision.
  const replaced = rows.filter((row) => row.fields['controlledBy'])

  const run = async () => {
    setBusy(true)
    const failed: Failure[] = []

    for (const row of rows) {
      try {
        await api.deleteObject(target.clusterId, target.typeKey, {
          name: row.name,
          ...(row.namespace ? { namespace: row.namespace } : {}),
        })
      } catch (error) {
        // Per object, not per batch: one blanket failure hides which three of ten
        // are still there.
        failed.push({
          name: row.name,
          reason: error instanceof ApiError ? error.message : 'could not be deleted',
        })
      }
    }

    setBusy(false)
    void queryClient.invalidateQueries({ queryKey: ['resources'] })
    if (failed.length < rows.length) onChanged()

    if (failed.length === 0) {
      onClose()
      return
    }
    setFailures(failed)
  }

  return (
    <ConfirmDialog
      destructive
      title={single ? `Delete ${kind} ${rows[0]?.name}` : `Delete ${rows.length} ${kind}`}
      confirmLabel="Delete"
      busy={busy}
      onConfirm={() => void run()}
      onCancel={onClose}
      {...(failures.length > 0
        ? { error: `${failures.length} could not be deleted; the rest are gone.` }
        : {})}
    >
      {/* The dialog names what is going and asks. Making someone retype the name they
          just clicked adds a step without adding a decision: they are looking at the
          object, and the list below says exactly what will go. */}
      <p>This cannot be undone.</p>

      {replaced.length > 0 && (
        <p className="mt-2" style={{ color: 'var(--status-warn)' }}>
          {replaced.length === rows.length
            ? `${single ? 'This' : 'These'} ${single ? 'is' : 'are'} managed by a controller`
            : `${replaced.length} of these are managed by a controller`}
          {single && replaced[0]?.fields['controlledByKind']
            ? ` (${replaced[0].fields['controlledByKind']} ${replaced[0].fields['controlledBy']})`
            : ''}
          . Deleting removes this instance; the controller will start a replacement.
        </p>
      )}

      <ul className="mt-2 max-h-48 overflow-auto font-mono" style={{ fontSize: 'var(--text-micro)' }}>
        {rows.map((row) => {
          const failure = failures.find((entry) => entry.name === row.name)
          return (
            <li key={`${row.namespace}/${row.name}`} style={{ color: failure ? 'var(--status-error)' : undefined }}>
              {row.namespace ? `${row.namespace}/` : ''}
              {row.name}
              {failure && ` — ${failure.reason}`}
            </li>
          )
        })}
      </ul>
    </ConfirmDialog>
  )
}
