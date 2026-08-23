import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ApiError, api } from '@/lib/api'

interface DrainDialogProps {
  clusterId: string
  node: string
  onChanged: () => void
  onClose: () => void
}

/**
 * Cordons a node and moves its pods off it.
 *
 * The plan is shown first because a drain is the most consequential thing here, and the
 * difference between "moves eleven pods" and "moves eleven pods and deletes the only
 * copy of a database" is visible in the plan and nowhere else. The node's name has to be
 * typed: this one is worth the extra second.
 */
export function DrainDialog({ clusterId, node, onChanged, onClose }: DrainDialogProps) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const plan = useQuery({
    queryKey: ['drain-plan', clusterId, node],
    queryFn: ({ signal }) => api.drainPlan(clusterId, node, signal),
  })

  const evict = plan.data?.evict ?? []
  const skip = plan.data?.skip ?? []
  const orphans = evict.filter((pod) => pod.reason)

  const run = async () => {
    setBusy(true)
    setError('')
    try {
      const result = await api.drainNode(clusterId, { name: node })
      void queryClient.invalidateQueries({ queryKey: ['resources'] })
      onChanged()

      const refused = result.results.filter((entry) => !entry.evicted)
      if (refused.length > 0) {
        // A budget that refuses is the system working, and which pods stayed is the
        // useful part of the answer.
        setError(
          `${refused.length} of ${result.results.length} could not be evicted: ` +
            refused.map((entry) => `${entry.name} (${entry.reason ?? 'refused'})`).join(', '),
        )
        return
      }
      onClose()
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : 'The cluster refused this.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <ConfirmDialog
      destructive
      title={`Drain node ${node}`}
      phrase={node}
      confirmLabel="Cordon and drain"
      busy={busy || plan.isLoading}
      {...(error ? { error } : {})}
      onConfirm={() => void run()}
      onCancel={onClose}
    >
      <p>
        The node is cordoned first, then its pods are asked to leave. A PodDisruptionBudget may
        refuse, which is the budget working.
      </p>

      {plan.isLoading && <p className="mt-2">Working out what would move…</p>}

      {plan.data && (
        <>
          <p className="mt-3" style={{ color: 'var(--text-primary)' }}>
            {evict.length} to move, {skip.length} staying.
          </p>

          {orphans.length > 0 && (
            <p className="mt-2" style={{ color: 'var(--status-error)' }}>
              {orphans.length} of them have no controller, so nothing will recreate them:{' '}
              {orphans.map((pod) => pod.name).join(', ')}.
            </p>
          )}

          <ul
            className="mt-2 max-h-40 overflow-auto font-mono"
            style={{ fontSize: 'var(--text-micro)' }}
          >
            {evict.map((pod) => (
              <li key={`${pod.namespace}/${pod.name}`}>
                {pod.namespace}/{pod.name}
                {pod.owner ? ` · ${pod.owner}` : ''}
              </li>
            ))}
            {skip.map((pod) => (
              <li key={`skip/${pod.namespace}/${pod.name}`} style={{ color: 'var(--text-muted)' }}>
                {pod.namespace}/{pod.name} — {pod.reason}
              </li>
            ))}
          </ul>
        </>
      )}
    </ConfirmDialog>
  )
}
