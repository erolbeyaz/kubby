/**
 * Colours a status value by what it means.
 *
 * Kubernetes phases and conditions are a small, known vocabulary, so the mapping is
 * explicit rather than guessed from substrings — "Terminating" and "Terminated" mean
 * different things, and a fuzzy match would colour them the same.
 */
const GOOD = new Set(['Running', 'Ready', 'Bound', 'Active', 'Succeeded', 'Available', 'True'])
const BUSY = new Set(['Pending', 'ContainerCreating', 'PodInitializing', 'Terminating', 'Progressing'])
const BAD = new Set([
  'Failed',
  'Error',
  'CrashLoopBackOff',
  'ImagePullBackOff',
  'ErrImagePull',
  'OOMKilled',
  'Evicted',
  'NotReady',
  'Unknown',
  'Lost',
  'Unbound',
])

export function statusColor(value: string): string {
  const head = value.split(',')[0] ?? value

  if (GOOD.has(head)) return 'var(--status-ok)'
  if (BUSY.has(head)) return 'var(--status-warn)'
  if (BAD.has(head)) return 'var(--status-error)'
  return 'var(--text-secondary)'
}

/** Maps a Kubernetes kind onto the resource type key the URL uses. */
export function typeKeyForKind(kind: string): string | null {
  const map: Record<string, string> = {
    Pod: 'pods',
    Deployment: 'apps/deployments',
    StatefulSet: 'apps/statefulsets',
    DaemonSet: 'apps/daemonsets',
    ReplicaSet: 'apps/replicasets',
    Job: 'batch/jobs',
    CronJob: 'batch/cronjobs',
    Node: 'nodes',
    Service: 'services',
  }
  return map[kind] ?? null
}
