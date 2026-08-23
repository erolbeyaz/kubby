export const SEVERITY_ORDER = ['critical', 'warning', 'info']

export const CATEGORY_LABELS: Record<string, string> = {
  workload: 'Workloads',
  // Kept apart from workloads on purpose: a mesh proxy restarting is not an application
  // outage, and mixing the two is how a routine restart reads as one (ADR-030).
  sidecar: 'Injected containers',
  node: 'Nodes',
  batch: 'Jobs',
  storage: 'Storage',
  event: 'Recent warnings',
  certificate: 'Certificates',
}

export function severityColour(severity: string): string {
  switch (severity) {
    case 'critical':
      return 'var(--status-error)'
    case 'warning':
      return 'var(--status-warn)'
    default:
      return 'var(--text-muted)'
  }
}
