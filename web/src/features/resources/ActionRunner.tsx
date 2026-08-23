import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ApiError, api, type ResourceRow } from '@/lib/api'

/** One action waiting for a yes. */
export interface PendingAction {
  id: string
  clusterId: string
  typeKey: string
  kind: string
  row: ResourceRow
}

interface ActionRunnerProps {
  action: PendingAction
  onChanged: () => void
  onClose: () => void
}

interface Wording {
  title: string
  body: string
  confirm: string
  destructive?: boolean
}

/**
 * Runs the actions that need nothing but a yes.
 *
 * Each one says what it will do in the cluster's terms rather than the button's: "roll
 * every pod" is what a restart is, and "restart" is only what it is called.
 */
export function ActionRunner({ action, onChanged, onClose }: ActionRunnerProps) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const { row, kind, typeKey, clusterId } = action
  const namespace = row.namespace ?? ''
  const suspended = row.fields['suspend'] === 'true'

  const wording = wordingFor(action.id, kind, row.name, suspended)

  const run = async () => {
    setBusy(true)
    setError('')
    try {
      switch (action.id) {
        case 'restart':
          await api.restart(clusterId, { typeKey, namespace, name: row.name })
          break
        case 'evict':
          await api.evict(clusterId, { namespace, name: row.name })
          break
        case 'trigger':
          await api.triggerCronJob(clusterId, { namespace, name: row.name })
          break
        case 'suspend':
          await api.suspendCronJob(clusterId, { namespace, name: row.name, suspended: !suspended })
          break
        case 'cordon':
          await api.cordonNode(clusterId, { name: row.name, unschedulable: !isCordoned(row) })
          break
        default:
          throw new Error(`${action.id} is not wired up`)
      }

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
      title={wording.title}
      confirmLabel={wording.confirm}
      busy={busy}
      {...(wording.destructive ? { destructive: true } : {})}
      {...(error ? { error } : {})}
      onConfirm={() => void run()}
      onCancel={onClose}
    >
      <p>{wording.body}</p>
    </ConfirmDialog>
  )
}

function isCordoned(row: ResourceRow): boolean {
  return (row.fields['status'] ?? '').includes('SchedulingDisabled')
}

function wordingFor(id: string, kind: string, name: string, suspended: boolean): Wording {
  switch (id) {
    case 'restart':
      return {
        title: `Restart ${kind} ${name}`,
        // What it does, not what it is called: a rollout replaces pods one at a time and
        // that is the part with consequences.
        body: 'Every pod is replaced, a few at a time, following the rollout strategy. Traffic keeps flowing if the readiness probes are honest.',
        confirm: 'Restart',
      }
    case 'evict':
      return {
        title: `Evict pod ${name}`,
        body: 'The cluster is asked to remove this pod, honouring any PodDisruptionBudget. A budget may refuse, which is the budget working.',
        confirm: 'Evict',
        destructive: true,
      }
    case 'trigger':
      return {
        title: `Run ${name} now`,
        body: 'A Job is created from this CronJob’s template and starts immediately. The schedule is not changed.',
        confirm: 'Run now',
      }
    case 'suspend':
      return suspended
        ? {
            title: `Resume ${name}`,
            body: 'The schedule starts running again from its next occurrence. Missed runs are not made up.',
            confirm: 'Resume',
          }
        : {
            title: `Suspend ${name}`,
            body: 'No further runs are started until it is resumed. Anything already running keeps going.',
            confirm: 'Suspend',
          }
    case 'cordon':
      return {
        title: `Cordon node ${name}`,
        body: 'The scheduler places no new pods here. Pods already running are left alone — moving them is what draining does.',
        confirm: 'Cordon',
      }
  }
  return { title: name, body: '', confirm: 'Confirm' }
}
