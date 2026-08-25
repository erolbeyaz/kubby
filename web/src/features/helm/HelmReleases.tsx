import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { Callout } from '@/components/Callout'
import { ApiError, api, type HelmRelease } from '@/lib/api'
import { formatAge } from '@/lib/time'

/**
 * What is installed in this cluster, as charts rather than as objects.
 *
 * A Deployment list answers "what is running". This answers "what did we install and at
 * which version", which is the question asked during an upgrade and the one the object
 * lists cannot answer at all — a chart's identity survives only in Helm's own records.
 */
export function HelmReleases({ clusterId, namespaces }: { clusterId: string; namespaces: string[] }) {
  const namespace = namespaces.length === 1 ? (namespaces[0] ?? '') : ''

  const releases = useQuery({
    queryKey: ['helm-releases', clusterId, namespace],
    queryFn: ({ signal }) => api.helmReleases(clusterId, namespace, signal),
    refetchInterval: 30_000,
  })

  const [opened, setOpened] = useState<HelmRelease | null>(null)
  const error = releases.error instanceof ApiError ? releases.error : null
  const rows = releases.data?.releases ?? []

  if (error) {
    return (
      <div className="p-4">
        <Callout tone="error" title="Could not read the releases" requestId={error.requestId}>
          {error.message}
        </Callout>
      </div>
    )
  }

  return (
    <div className="flex h-full min-w-0">
      <div className="min-h-0 min-w-0 flex-1 overflow-auto">
        <table className="w-full border-collapse" style={{ fontSize: 'var(--text-secondary-size)' }}>
          <thead>
            <tr
              className="sticky top-0 z-10"
              style={{ backgroundColor: 'var(--bg-surface)', fontSize: 'var(--text-micro)' }}
            >
              {['Release', 'Namespace', 'Chart', 'App version', 'Status', 'Rev', 'Updated'].map((label) => (
                <th
                  key={label}
                  className="border-b px-3 py-1.5 text-left font-semibold uppercase tracking-[0.06em]"
                  style={{ borderColor: 'var(--border-subtle)', color: 'var(--text-muted)' }}
                >
                  {label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((release) => (
              <tr
                key={`${release.namespace}/${release.name}`}
                onClick={() => setOpened(release)}
                className="cursor-pointer border-b transition-colors hover:bg-[var(--bg-hover)]"
                style={{
                  borderColor: 'var(--border-subtle)',
                  backgroundColor:
                    opened?.name === release.name && opened.namespace === release.namespace
                      ? 'var(--bg-active)'
                      : undefined,
                }}
              >
                <td className="px-3 py-1.5" style={{ color: 'var(--text-primary)' }}>
                  {release.name}
                </td>
                <td className="px-3 py-1.5 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                  {release.namespace}
                </td>
                <td className="px-3 py-1.5 font-mono" style={{ fontSize: 'var(--text-micro)' }}>
                  {release.chart}
                  {release.chartVersion ? (
                    <span style={{ color: 'var(--text-muted)' }}> {release.chartVersion}</span>
                  ) : null}
                </td>
                <td className="px-3 py-1.5 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>
                  {release.appVersion ?? '—'}
                </td>
                <td className="px-3 py-1.5" style={{ fontSize: 'var(--text-micro)', color: statusColour(release.status) }}>
                  {release.status}
                </td>
                <td className="px-3 py-1.5 font-mono tabular-nums" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                  {release.revision}
                </td>
                <td className="px-3 py-1.5 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                  {release.updated ? formatAge(release.updated) : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {!releases.isPending && rows.length === 0 && (
          <p className="p-6 text-center" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            {/* Not an error: plenty of clusters are managed entirely without Helm. */}
            No Helm releases in this cluster.
          </p>
        )}
      </div>

      {opened && (
        <ReleaseDetail
          clusterId={clusterId}
          release={opened}
          onClose={() => setOpened(null)}
        />
      )}
    </div>
  )
}

function ReleaseDetail({
  clusterId,
  release,
  onClose,
}: {
  clusterId: string
  release: HelmRelease
  onClose: () => void
}) {
  const detail = useQuery({
    queryKey: ['helm-release', clusterId, release.namespace, release.name],
    queryFn: ({ signal }) => api.helmRelease(clusterId, release.namespace, release.name, signal),
  })

  const values = detail.data?.values
  const history = detail.data?.history ?? []

  return (
    <aside
      className="flex w-[26rem] shrink-0 flex-col overflow-auto border-l"
      style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
    >
      <header
        className="flex shrink-0 items-center gap-2 border-b px-3 py-2"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <span className="min-w-0 flex-1 truncate" style={{ color: 'var(--text-primary)' }}>
          {release.name}
        </span>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="tool-button"
          style={{ color: 'var(--text-muted)' }}
        >
          ✕
        </button>
      </header>

      <Section title="Values">
        {detail.isPending ? (
          <Muted>reading…</Muted>
        ) : values && Object.keys(values).length > 0 ? (
          <pre
            className="overflow-auto whitespace-pre-wrap break-words font-mono"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
          >
            {JSON.stringify(values, null, 2)}
          </pre>
        ) : (
          // Meaningfully different from "no values": the chart's own defaults were used,
          // which is the answer someone comparing two installs needs.
          <Muted>installed with the chart's defaults — nothing was overridden</Muted>
        )}
      </Section>

      <Section title={`History (${history.length})`}>
        {history.length === 0 ? (
          <Muted>no retained revisions</Muted>
        ) : (
          <ul>
            {history.map((revision) => (
              <li
                key={revision.revision}
                className="flex items-baseline gap-2 border-b py-1 last:border-b-0"
                style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
              >
                <span className="w-8 shrink-0 font-mono tabular-nums" style={{ color: 'var(--text-muted)' }}>
                  {revision.revision}
                </span>
                <span className="w-20 shrink-0" style={{ color: statusColour(revision.status) }}>
                  {revision.status}
                </span>
                <span className="min-w-0 flex-1 truncate" style={{ color: 'var(--text-secondary)' }}>
                  {revision.description}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Section>

      {detail.data?.notes && (
        <Section title="Notes">
          <pre
            className="overflow-auto whitespace-pre-wrap break-words font-mono"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
          >
            {detail.data.notes}
          </pre>
        </Section>
      )}
    </aside>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-b p-3" style={{ borderColor: 'var(--border-subtle)' }}>
      <h4
        className="mb-1.5 font-semibold uppercase tracking-[0.08em]"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        {title}
      </h4>
      {children}
    </section>
  )
}

function Muted({ children }: { children: React.ReactNode }) {
  return (
    <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>{children}</p>
  )
}

// Helm's own vocabulary. `failed` and `pending-*` are the ones worth spotting from across
// the table: a release stuck pending is an upgrade that never finished.
function statusColour(status: string): string {
  if (status === 'deployed') return 'var(--status-ok)'
  if (status === 'failed' || status === 'unknown') return 'var(--status-error)'
  if (status.startsWith('pending') || status === 'superseded') return 'var(--status-warn)'
  return 'var(--text-secondary)'
}
