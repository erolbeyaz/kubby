import { useQuery } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'

import { api, type ClusterHealthMetrics } from '@/lib/api'

import {
  AreaChart,
  BarColumns,
  BarRows,
  Donut,
  SERIES_COLOURS,
  StatTile,
  format,
  formatBytes,
} from './charts'

const WINDOWS = ['15m', '1h', '6h', '24h'] as const
type Window = (typeof WINDOWS)[number]

/**
 * The cluster at a glance.
 *
 * Laid out so the first row answers "is anything wrong" without reading a word — the
 * numbers are large, and the ones that are supposed to be zero are coloured only when
 * they are not. Everything below is the detail behind that answer, and everything on the
 * screen comes from Prometheus, because none of it is a question the Kubernetes API can
 * answer about the past.
 *
 * Colour carries meaning and nothing else: orange is worth a look, red is worth acting
 * on, and a healthy cluster is almost entirely green and grey.
 */
export function ClusterDashboard({ clusterId }: { clusterId: string }) {
  const [window, setWindow] = useState<Window>('1h')

  const metrics = useQuery({
    queryKey: ['cluster-metrics', clusterId, window],
    queryFn: ({ signal }) => api.clusterMetrics(clusterId, window, signal),
    refetchInterval: 30_000,
  })

  if (metrics.data && !metrics.data.configured) return <NotConfigured />
  if (metrics.data?.error) return <Unreachable message={metrics.data.error} />

  const health = metrics.data?.health
  const problems = countProblems(health)

  return (
    <div className="flex flex-col gap-3">
      <header className="flex items-center gap-3">
        <Verdict problems={problems} loading={metrics.isPending} />
        <span className="ml-auto flex items-center gap-1">
          {WINDOWS.map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setWindow(option)}
              className="tool-chip font-mono"
              style={{
                borderColor: option === window ? 'var(--accent)' : 'var(--border-default)',
                color: option === window ? 'var(--accent)' : 'var(--text-muted)',
              }}
            >
              {option}
            </button>
          ))}
        </span>
      </header>

      {/* The headline row. Failed and pending are grey at zero and coloured only when
          they are not, so a quiet cluster does not shout. */}
      <div
        className="grid gap-px"
        style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(9rem, 1fr))', backgroundColor: 'var(--border-subtle)' }}
      >
        <StatTile
          label="Pods running"
          value={format(health?.pods.running ?? 0)}
          sub={`of ${format(health?.pods ? total(health.pods) : 0)}`}
          tone="var(--status-ok)"
        />
        <StatTile
          label="Pods pending"
          value={format(health?.pods.pending ?? 0)}
          sub="waiting to start"
          tone={(health?.pods.pending ?? 0) > 0 ? 'var(--status-warn)' : 'var(--status-unknown)'}
        />
        <StatTile
          label="Pods failed"
          value={format(health?.pods.failed ?? 0)}
          sub="gave up"
          tone={(health?.pods.failed ?? 0) > 0 ? 'var(--status-error)' : 'var(--status-unknown)'}
        />
        <StatTile
          label="Nodes ready"
          value={`${format(health?.nodes.ready ?? 0)}/${format(health?.nodes.total ?? 0)}`}
          sub="reporting Ready"
          tone={
            health && health.nodes.ready < health.nodes.total
              ? 'var(--status-error)'
              : 'var(--status-ok)'
          }
        />
        <StatTile
          label="Restarts today"
          value={format(health?.restarts24h ?? 0)}
          sub="last 24 hours"
          tone={(health?.restarts24h ?? 0) > 10 ? 'var(--status-error)' : 'var(--accent)'}
        />
      </div>

      <div
        className="grid gap-3"
        style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(20rem, 1fr))' }}
      >
        <Card title="Pods by phase">
          <Donut slices={phaseSlices(health)} centreLabel="pods" />
        </Card>

        <Card title="CPU per node" hint="what the machines are doing, not what pods asked for">
          <BarColumns bars={toBars(health?.cpuByNode)} toneOf={(v) => gauge(v, 70, 90)} />
        </Card>

        <Card title="Memory per node">
          <BarColumns bars={toBars(health?.memoryByNode)} toneOf={(v) => gauge(v, 75, 90)} />
        </Card>
      </div>

      <div
        className="grid gap-3"
        style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(20rem, 1fr))' }}
      >
        <Card title={`CPU and memory · last ${window}`}>
          <AreaChart
            unit="%"
            series={[
              { name: 'CPU', colour: 'var(--accent)', points: health?.cpu ?? [] },
              { name: 'Memory', colour: 'var(--status-info)', points: health?.memory ?? [] },
            ]}
          />
        </Card>

        <Card title={`Container restarts · last ${window}`} hint="per hour">
          <AreaChart
            unit="/h"
            series={[
              { name: 'Restarts', colour: 'var(--status-warn)', points: health?.restarts ?? [] },
            ]}
          />
        </Card>
      </div>

      <div
        className="grid gap-3"
        style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(20rem, 1fr))' }}
      >
        <Card title="Busiest namespaces · CPU">
          <BarRows bars={toNamed(health?.topCpu)} format={(v) => `${format(v)}m`} />
        </Card>

        <Card title="Busiest namespaces · memory">
          <BarRows bars={toNamed(health?.topMemory)} format={formatBytes} />
        </Card>

        <Card title="Node disk · fullest filesystem">
          <BarRows
            bars={(health?.disk ?? []).map((d) => ({ name: d.node, value: d.percent }))}
            format={(v) => `${Math.round(v)}%`}
          />
        </Card>
      </div>

      {((health?.waiting?.length ?? 0) > 0 || (health?.reasons?.length ?? 0) > 0) && (
        <div
          className="grid gap-3"
          style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(20rem, 1fr))' }}
        >
          {(health?.waiting?.length ?? 0) > 0 && (
            <Card title="Containers stuck" hint="why they will not start">
              <Donut slices={reasonSlices(health?.waiting)} centreLabel="stuck" />
            </Card>
          )}
          {(health?.reasons?.length ?? 0) > 0 && (
            <Card title="Why containers died" hint="the kubelet's own reason">
              <Donut slices={reasonSlices(health?.reasons)} centreLabel="killed" />
            </Card>
          )}
        </div>
      )}

      {(health?.failing?.length ?? 0) > 0 && (
        <Card title="Containers that keep dying" tone="error">
          <div className="flex flex-col">
            {health?.failing?.map((entry) => (
              <Row
                key={`${entry.namespace}/${entry.pod}/${entry.container}`}
                tone={entry.reason === 'OOMKilled' ? 'error' : 'warn'}
                left={`${entry.namespace} / ${entry.pod}`}
                middle={entry.container}
                right={`${Math.round(entry.restarts)}× ${entry.reason ? `· ${entry.reason}` : ''}`}
              />
            ))}
          </div>
        </Card>
      )}

      {(health?.degraded?.length ?? 0) > 0 && (
        <Card title="Short of replicas" tone="warn">
          <div className="flex flex-col">
            {health?.degraded?.map((entry) => (
              <Row
                key={`${entry.namespace}/${entry.name}`}
                // Under two minutes is a rollout in progress, not a fault.
                tone={entry.forMinutes > 2 ? 'error' : 'warn'}
                left={`${entry.namespace} / ${entry.name}`}
                middle={`${Math.round(entry.missing)} missing`}
                right={forHuman(entry.forMinutes)}
              />
            ))}
          </div>
        </Card>
      )}

      {(health?.nodeIssues?.length ?? 0) > 0 && (
        <Card title="Node conditions" tone="error">
          <div className="flex flex-col">
            {health?.nodeIssues?.map((entry) => (
              <Row
                key={`${entry.node}/${entry.condition}`}
                tone="error"
                left={entry.node}
                middle={entry.condition}
                right={forHuman(entry.minutes)}
              />
            ))}
          </div>
        </Card>
      )}

      {(health?.warnings?.length ?? 0) > 0 && (
        <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {/* A partial dashboard must never pass as a complete one. */}
          Some readings are missing: {health?.warnings?.join(' · ')}
        </p>
      )}
    </div>
  )
}

function Card({
  title,
  hint,
  tone,
  children,
}: {
  title: string
  hint?: string
  tone?: 'warn' | 'error'
  children: ReactNode
}) {
  const edge = tone === 'error' ? 'var(--status-error)' : tone === 'warn' ? 'var(--status-warn)' : undefined

  return (
    <section
      className="border"
      style={{
        borderColor: 'var(--border-subtle)',
        backgroundColor: 'var(--bg-surface)',
        ...(edge ? { borderLeft: `2px solid ${edge}` } : {}),
      }}
    >
      <header
        className="flex items-baseline gap-2 border-b px-3 py-1.5"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <h4
          className="font-semibold uppercase tracking-[0.08em]"
          style={{ fontSize: 'var(--text-micro)', color: edge ?? 'var(--text-secondary)' }}
        >
          {title}
        </h4>
        {hint && (
          <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>{hint}</span>
        )}
      </header>
      <div className="p-3">{children}</div>
    </section>
  )
}

function Row({
  tone,
  left,
  middle,
  right,
}: {
  tone: 'warn' | 'error'
  left: string
  middle: string
  right: string
}) {
  const colour = tone === 'error' ? 'var(--status-error)' : 'var(--status-warn)'

  return (
    <div
      className="flex items-center gap-3 border-b py-1.5 last:border-b-0"
      style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-secondary-size)' }}
    >
      <span
        aria-hidden="true"
        className="h-1.5 w-1.5 shrink-0 rounded-full"
        style={{ backgroundColor: colour }}
      />
      <span className="min-w-0 flex-1 truncate" style={{ color: 'var(--text-primary)' }}>
        {left}
      </span>
      <span className="shrink-0 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {middle}
      </span>
      <span className="shrink-0 font-mono tabular-nums" style={{ fontSize: 'var(--text-micro)', color: colour }}>
        {right}
      </span>
    </div>
  )
}

function Verdict({ problems, loading }: { problems: number; loading: boolean }) {
  if (loading) {
    return <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>reading…</span>
  }

  const ok = problems === 0
  return (
    <span
      className="flex items-center gap-2 px-2.5 py-1"
      style={{
        borderRadius: 'var(--radius-sharp)',
        fontSize: 'var(--text-secondary-size)',
        color: ok ? 'var(--status-ok)' : 'var(--status-error)',
        backgroundColor: `color-mix(in srgb, ${ok ? 'var(--status-ok)' : 'var(--status-error)'} 12%, transparent)`,
      }}
    >
      <span
        aria-hidden="true"
        className="inline-block h-2 w-2 rounded-full"
        style={{ backgroundColor: ok ? 'var(--status-ok)' : 'var(--status-error)' }}
      />
      {ok ? 'Everything is healthy' : `${problems} thing${problems === 1 ? '' : 's'} need attention`}
    </span>
  )
}

// Phase colours are fixed rather than taken from the palette in order: Running must be
// green and Failed red on every cluster, or the ring means nothing at a glance.
const PHASE_COLOURS: Record<string, string> = {
  Running: 'var(--status-ok)',
  Pending: 'var(--status-warn)',
  Failed: 'var(--status-error)',
  Succeeded: 'var(--status-info)',
  Unknown: 'var(--status-unknown)',
}

function phaseSlices(health: ClusterHealthMetrics | undefined) {
  const pods = health?.pods
  if (!pods) return []

  return (
    [
      { name: 'Running', value: pods.running },
      { name: 'Pending', value: pods.pending },
      { name: 'Failed', value: pods.failed },
      { name: 'Succeeded', value: pods.succeeded },
      { name: 'Unknown', value: pods.unknown },
    ] as const
  )
    .filter((slice) => slice.value > 0)
    .map((slice) => ({ ...slice, colour: PHASE_COLOURS[slice.name] ?? 'var(--status-unknown)' }))
}

// A reason people already read as bad keeps its colour wherever it appears.
const REASON_COLOURS: Record<string, string> = {
  OOMKilled: 'var(--status-error)',
  Error: 'var(--status-error)',
  CrashLoopBackOff: 'var(--status-error)',
  ImagePullBackOff: 'var(--status-warn)',
  ErrImagePull: 'var(--status-warn)',
  ContainerCreating: 'var(--status-info)',
}

function reasonSlices(values: { name: string; value: number }[] | null | undefined) {
  return (values ?? []).map((entry, index) => ({
    ...entry,
    colour: REASON_COLOURS[entry.name] ?? SERIES_COLOURS[index % SERIES_COLOURS.length] ?? 'var(--accent)',
  }))
}

function toBars(gauges: { node: string; percent: number }[] | null | undefined) {
  return (gauges ?? []).map((gaugeValue) => ({ name: gaugeValue.node, value: gaugeValue.percent }))
}

function toNamed(values: { name: string; value: number }[] | null | undefined) {
  return values ?? []
}

function total(pods: ClusterHealthMetrics['pods']): number {
  return pods.running + pods.pending + pods.failed + pods.succeeded + pods.unknown
}

function gauge(value: number, warnAt: number, errorAt: number): string {
  if (value >= errorAt) return 'var(--status-error)'
  if (value >= warnAt) return 'var(--status-warn)'
  return 'var(--status-ok)'
}

function countProblems(health: ClusterHealthMetrics | undefined): number {
  if (!health) return 0
  return (
    (health.failing?.length ?? 0) + (health.degraded?.length ?? 0) + (health.nodeIssues?.length ?? 0)
  )
}

function forHuman(minutes: number): string {
  if (minutes < 1) return 'just now'
  if (minutes < 60) return `${Math.round(minutes)}m`
  if (minutes < 60 * 24) return `${(minutes / 60).toFixed(1)}h`
  return `${Math.round(minutes / (60 * 24))}d`
}

function NotConfigured() {
  return (
    <section
      className="border p-4"
      style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
    >
      <h3 style={{ fontSize: 'var(--text-body)', color: 'var(--text-primary)' }}>
        No metrics endpoint for this cluster
      </h3>
      <p className="mt-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        The Kubernetes API can say what is true now. History — whether restarts are
        accelerating, how long a workload has been degraded, how full the nodes' disks are
        — needs a Prometheus. Add its address under the cluster's settings.
      </p>
    </section>
  )
}

function Unreachable({ message }: { message: string }) {
  return (
    <section className="border p-4" style={{ borderColor: 'var(--status-warn)' }}>
      <h3 style={{ fontSize: 'var(--text-body)', color: 'var(--status-warn)' }}>
        The metrics endpoint did not answer
      </h3>
      <p className="mt-1 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>
        {message}
      </p>
    </section>
  )
}
