import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { ApiError, api, type Cluster, type Gauge } from '@/lib/api'

import { FleetHealth } from '@/features/health/FleetHealth'

import { Donut } from './Donut'
import type { NavigationTarget } from './ResourceTable'
import { typeKeyForKind } from './statusColor'

interface ClusterOverviewProps {
  cluster: Cluster
  onNavigate: (target: NavigationTarget) => void
  onOpenCluster: (clusterId: string) => void
}

/** The cluster at a glance: how full it is, and whether anything is wrong. */
export function ClusterOverview({ cluster, onNavigate, onOpenCluster }: ClusterOverviewProps) {
  const overview = useQuery({
    queryKey: ['overview', cluster.id],
    queryFn: ({ signal }) => api.overview(cluster.id, signal),
    refetchInterval: 20_000,
  })

  const error = overview.error instanceof ApiError ? overview.error : null
  const data = overview.data

  if (error) {
    return (
      <div className="p-5">
        <Callout tone="error" title="Could not read the cluster" requestId={error.requestId}>
          {error.message}
        </Callout>
      </div>
    )
  }

  const problems = data?.problems ?? []

  return (
    <div className="h-full overflow-auto">
      {/* The rest of the fleet, above this cluster's own numbers. Reading one cluster
          with the others out of view is how a fleet-wide outage reads as a single
          cluster's problem. */}
      <div className="border-b px-4 pb-2 pt-3" style={{ borderColor: 'var(--border-subtle)' }}>
        <FleetHealth compact currentId={cluster.id} onOpen={onOpenCluster} />
      </div>

      <div
        className="grid gap-px border-b"
        style={{
          gridTemplateColumns: 'repeat(auto-fit, minmax(15rem, 1fr))',
          borderColor: 'var(--border-subtle)',
          backgroundColor: 'var(--border-subtle)',
        }}
      >
        <Panel>
          <Donut
            title="CPU"
            total={data?.cpu.allocatable ?? 0}
            segments={gaugeSegments(data?.cpu)}
            format={(value) => `${value.toFixed(2)} cores`}
          />
        </Panel>
        <Panel>
          <Donut
            title="Memory"
            total={data?.memory.allocatable ?? 0}
            segments={gaugeSegments(data?.memory)}
            format={formatMiB}
          />
        </Panel>
        <Panel>
          <Donut
            title="Pods"
            total={data?.pods.allocatable ?? 0}
            segments={[
              { label: 'Running', value: data?.pods.usage ?? 0, color: 'var(--status-ok)' },
            ]}
            format={(value) => String(Math.round(value))}
          />
        </Panel>
        <Panel>
          <div className="flex h-full flex-col justify-center gap-2.5">
            <Fact label="Kubernetes" value={data?.k8sVersion || cluster.k8sVersion || '—'} />
            <Fact
              label="Nodes"
              value={data ? `${data.nodesReady}/${data.nodes} ready` : '—'}
              tone={data && data.nodesReady < data.nodes ? 'var(--status-error)' : undefined}
              onClick={() => onNavigate({ typeKey: 'nodes', namespace: '' })}
            />
            <Fact
              label="Namespaces"
              value={data ? String(data.namespaces) : '—'}
              onClick={() => onNavigate({ typeKey: 'namespaces', namespace: '' })}
            />
            <Fact
              label="Metrics API"
              value={data?.metricsAvailable ? 'available' : 'not installed'}
              tone={data && !data.metricsAvailable ? 'var(--status-warn)' : undefined}
            />
          </div>
        </Panel>
      </div>

      <div className="p-5">
        {problems.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-16">
            <svg width="40" height="40" viewBox="0 0 40 40" aria-hidden="true">
              <circle cx="20" cy="20" r="18" fill="none" stroke="var(--status-ok)" strokeWidth="2" />
              <path
                d="M12 20.5 L17.5 26 L28 15"
                fill="none"
                stroke="var(--status-ok)"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            <p style={{ fontSize: 'var(--text-body)', color: 'var(--text-secondary)' }}>Nothing needs attention</p>
            <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
              Every node is ready and no pod is failing.
            </p>
          </div>
        ) : (
          <section>
            <h3
              className="mb-2 font-semibold uppercase tracking-[0.08em]"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            >
              Needs attention ({problems.length})
            </h3>

            <div className="flex flex-col">
              {problems.map((problem) => {
                const target = typeKeyForKind(problem.kind)
                return (
                  <button
                    key={`${problem.kind}/${problem.namespace ?? ''}/${problem.name}`}
                    type="button"
                    disabled={!target}
                    onClick={
                      target
                        ? () =>
                            onNavigate({
                              typeKey: target,
                              namespace: problem.namespace ?? '',
                              objectName: problem.name,
                            })
                        : undefined
                    }
                    className="flex items-center gap-3 border-b px-2 py-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
                    style={{ borderColor: 'var(--border-subtle)' }}
                  >
                    <span
                      className="h-1.5 w-1.5 shrink-0 rounded-full"
                      style={{
                        backgroundColor:
                          problem.severity === 'error' ? 'var(--status-error)' : 'var(--status-warn)',
                      }}
                    />
                    <span
                      className="w-20 shrink-0 font-mono"
                      style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
                    >
                      {problem.kind}
                    </span>
                    <span
                      className="min-w-0 flex-1 truncate"
                      style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}
                    >
                      {problem.namespace ? `${problem.namespace} / ` : ''}
                      {problem.name}
                    </span>
                    <span
                      style={{
                        fontSize: 'var(--text-micro)',
                        color: problem.severity === 'error' ? 'var(--status-error)' : 'var(--status-warn)',
                      }}
                    >
                      {problem.reason}
                    </span>
                  </button>
                )
              })}
            </div>

            <p className="mt-3" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
              The full health panel — restart analysis, image pull failures, unbound
              volumes, certificate expiry — arrives in phase 5.
            </p>
          </section>
        )}
      </div>
    </div>
  )
}

function Panel({ children }: { children: React.ReactNode }) {
  return (
    <div className="p-5" style={{ backgroundColor: 'var(--bg-surface)' }}>
      {children}
    </div>
  )
}

function Fact({
  label,
  value,
  tone,
  onClick,
}: {
  label: string
  value: string
  tone?: string | undefined
  onClick?: (() => void) | undefined
}) {
  const content = (
    <>
      <span style={{ color: 'var(--text-muted)' }}>{label}</span>
      <span className="font-mono" style={{ color: tone ?? 'var(--text-primary)' }}>
        {value}
      </span>
    </>
  )

  if (!onClick) {
    return (
      <div className="flex items-baseline justify-between gap-3" style={{ fontSize: 'var(--text-micro)' }}>
        {content}
      </div>
    )
  }

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-baseline justify-between gap-3 hover:underline"
      style={{ fontSize: 'var(--text-micro)' }}
    >
      {content}
    </button>
  )
}

/**
 * Usage, requests and limits share an origin because they overlap: usage happens inside
 * a request, and a request sits under a limit.
 */
function gaugeSegments(gauge: Gauge | undefined) {
  if (!gauge) return []

  return [
    { label: 'Usage', value: gauge.usage, color: 'var(--accent)' },
    { label: 'Requests', value: gauge.requests, color: 'var(--status-info)' },
    { label: 'Limits', value: gauge.limits, color: 'var(--env-dr)' },
  ]
}

function formatMiB(value: number): string {
  if (value >= 1024) return `${(value / 1024).toFixed(1)} GiB`
  return `${Math.round(value)} MiB`
}
