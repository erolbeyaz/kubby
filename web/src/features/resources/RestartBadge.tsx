import { useQuery } from '@tanstack/react-query'

import { api, type RestartSummary } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

interface RestartBadgeProps {
  clusterId: string
  namespace: string
  pod: string
  /** Show the per-container breakdown rather than only the badge. */
  detailed?: boolean
}

/**
 * Why a pod restarted, not just how often.
 *
 * The count is the workload's own; anything a platform injected is stated separately
 * (ADR-030 §3). A single total that folds istio-proxy's start-up restarts into the
 * application's is a correct number that sends the reader to the wrong place.
 */
export function RestartBadge({ clusterId, namespace, pod, detailed = false }: RestartBadgeProps) {
  const restarts = useQuery({
    queryKey: ['restarts', clusterId, namespace, pod],
    queryFn: ({ signal }) => api.podRestarts(clusterId, namespace, pod, signal),
    staleTime: 15_000,
  })

  if (!restarts.data) {
    return (
      <p className="px-3 py-1.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {restarts.isLoading ? 'Reading restart history…' : '—'}
      </p>
    )
  }

  const summary = restarts.data
  if (!detailed) return <Badge summary={summary} />

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 px-3 py-1.5">
        <Badge summary={summary} />
        {summary.app === 0 && summary.sidecar === 0 && summary.init === 0 && (
          <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            Nothing has restarted.
          </span>
        )}
      </div>

      {(summary.details ?? [])
        .filter((container) => container.count > 0 || container.last)
        .map((container) => (
          <div
            key={container.name}
            className="border-t px-3 py-1.5"
            style={{ borderColor: 'var(--border-subtle)' }}
          >
            <div className="flex items-center gap-2">
              <span
                className="min-w-0 flex-1 truncate font-mono"
                style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
              >
                {container.name}
              </span>
              {container.role !== 'app' && (
                <span
                  className="px-1"
                  style={{
                    fontSize: 'var(--text-micro)',
                    color: 'var(--text-muted)',
                    borderRadius: 'var(--radius-sharp)',
                    backgroundColor: 'var(--bg-hover)',
                  }}
                >
                  {container.role}
                </span>
              )}
              <span
                className="font-mono"
                style={{
                  fontSize: 'var(--text-micro)',
                  color: container.count > 0 ? 'var(--status-warn)' : 'var(--text-muted)',
                }}
              >
                {container.count}×
              </span>
            </div>

            {container.last && (
              <p className="mt-0.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                {explain(container.last)}
                {container.last.finishedAt && (
                  <>
                    {' '}
                    <span title={formatAbsolute(container.last.finishedAt)}>
                      {formatAge(container.last.finishedAt)} ago
                    </span>
                  </>
                )}
              </p>
            )}
          </div>
        ))}
    </div>
  )
}

function Badge({ summary }: { summary: RestartSummary }) {
  return (
    <span className="flex items-center gap-1.5">
      <span
        className="font-mono"
        style={{
          fontSize: 'var(--text-secondary-size)',
          color: summary.app > 0 ? 'var(--status-warn)' : 'var(--text-secondary)',
        }}
      >
        {summary.app}
      </span>
      {summary.sidecar > 0 && (
        <span
          title="Restarts of containers a platform injected, counted apart from the workload's own"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
        >
          +{summary.sidecar} sidecar
        </span>
      )}
      {summary.init > 0 && (
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          +{summary.init} init
        </span>
      )}
    </span>
  )
}

type Termination = NonNullable<NonNullable<RestartSummary['details']>[number]['last']>

/** Reading a restart should not require knowing that 137 means the kernel killed it. */
function explain(last: Termination): string {
  if (last.reason === 'OOMKilled') return 'Killed for exceeding its memory limit.'
  if (last.reason === 'Completed' && last.exitCode === 0) return 'Exited normally.'
  if (last.exitCode === 137) return 'Exit code 137: killed by SIGKILL, most often the memory limit.'
  if (last.exitCode === 143) return 'Exit code 143: terminated by SIGTERM during shutdown.'
  if (last.exitCode === 1) return 'Exited with code 1: the process failed. Its last log lines say why.'
  if (last.exitCode !== 0) return `Exited with code ${last.exitCode}.`
  return last.reason ?? ''
}
