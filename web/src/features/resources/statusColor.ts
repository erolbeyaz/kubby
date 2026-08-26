/**
 * Colours a status value by what it means.
 *
 * Kubernetes phases and conditions are a small, known vocabulary, so the mapping is
 * explicit rather than guessed from substrings — "Terminating" and "Terminated" mean
 * different things, and a fuzzy match would colour them the same.
 */
const GOOD = new Set(['Running', 'Ready', 'Bound', 'Active', 'Succeeded', 'Available', 'True', 'Normal', 'Completed'])
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
  // An event's own type. A Warning is the reason the events screen gets opened, so it
  // is coloured like the trouble it reports rather than like a category label.
  'Warning',
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
    PersistentVolumeClaim: 'persistentvolumeclaims',
    PersistentVolume: 'persistentvolumes',
    Namespace: 'namespaces',
  }
  return map[kind] ?? null
}
