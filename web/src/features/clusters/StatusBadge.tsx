import type { CredentialStatus } from '@/lib/api'

const LABEL: Record<CredentialStatus, string> = {
  valid: 'Connected',
  invalid: 'Credential invalid',
  unreachable: 'Unreachable',
  unknown: 'Not checked',
}

const COLOR: Record<CredentialStatus, string> = {
  valid: 'var(--status-ok)',
  invalid: 'var(--status-error)',
  unreachable: 'var(--status-warn)',
  unknown: 'var(--status-unknown)',
}

/** One glance should say whether a cluster is usable and, if not, whose problem it is. */
export function StatusBadge({ status, detail }: { status: CredentialStatus; detail?: string | undefined }) {
  return (
    <span
      className="inline-flex items-center gap-1.5 whitespace-nowrap"
      style={{ fontSize: 'var(--text-micro)', color: COLOR[status] }}
      title={detail || LABEL[status]}
    >
      <span
        aria-hidden="true"
        className="inline-block h-1.5 w-1.5 rounded-full"
        style={{ backgroundColor: COLOR[status] }}
      />
      {LABEL[status]}
    </span>
  )
}
