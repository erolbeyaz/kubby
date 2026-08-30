import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { api, type PodMetrics, type ResourceRow } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

import { AreaChart, SERIES_COLOURS, formatBytes } from '@/features/metrics/charts'

import {
  containersOf,
  formatQuantities,
  podSpecOf,
  pulledFrom,
  registryOf,
  volumesOf,
  type ContainerSpec,
} from './containers'
import { ForwardablePort } from './ForwardablePort'
import { statusColor, typeKeyForKind } from './statusColor'
import type { NavigationTarget } from './ResourceTable'

/** One container's own history, as the pod metrics endpoint returns it. */
type ContainerUsage = NonNullable<NonNullable<PodMetrics['usage']>['containers']>[string]

interface PodDetailProps {
  clusterId: string
  typeKey: string
  row: ResourceRow
  object: Record<string, unknown>
  metric: 'cpu' | 'memory'
  onNavigate: (target: NavigationTarget) => void
}

/**
 * A pod, read the way someone asks about one.
 *
 * What is it using, what is it, what is inside it, and what has happened to it. The
 * containers carry their own charts because a pod with a sidecar answers "which of them"
 * and the pod's total never does.
 */
export function PodDetail({ clusterId, typeKey, row, object, metric, onNavigate }: PodDetailProps) {
  const namespace = row.namespace ?? ''
  const metadata = (object.metadata ?? {}) as Record<string, unknown>
  const spec = (object.spec ?? {}) as Record<string, unknown>
  const status = (object.status ?? {}) as Record<string, unknown>

  const metrics = useQuery({
    queryKey: ['pod-metrics', clusterId, namespace, row.name, '1h'],
    queryFn: ({ signal }) => api.podMetrics(clusterId, namespace, row.name, '1h', signal),
    staleTime: 30_000,
  })

  const events = useQuery({
    queryKey: ['resources', clusterId, 'events', row.name],
    queryFn: ({ signal }) => api.resources(clusterId, 'events', { search: row.name }, signal),
    staleTime: 15_000,
  })

  const containers = containersOf(podSpecOf('Pod', spec))
  const volumes = volumesOf(podSpecOf('Pod', spec))
  const owner = row.fields['controlledBy'] ?? ''
  const ownerKind = row.fields['controlledByKind'] ?? ''
  const ownerTarget = ownerKind ? typeKeyForKind(ownerKind) : null

  const about = (events.data?.rows ?? []).filter((event) =>
    (event.fields['involvedObject'] ?? '').includes(row.name),
  )

  return (
    <div className="flex flex-col">
      <Usage title={metric === 'cpu' ? 'CPU' : 'Memory'} metric={metric} metrics={metrics.data} />

      <Section title="Properties">
        <Property label="Created">
          {row.createdAt ? `${formatAge(row.createdAt)} ago · ${formatAbsolute(row.createdAt)}` : '—'}
        </Property>
        <Property label="Name">{row.name}</Property>
        <Property label="Namespace">
          <Link onClick={() => onNavigate({ namespace })}>{namespace}</Link>
        </Property>
        <Chips label="Labels" values={metadata.labels} />
        <Chips label="Annotations" values={metadata.annotations} />
        {owner && (
          <Property label="Controlled By">
            <span style={{ color: 'var(--text-muted)' }}>{ownerKind} </span>
            {ownerTarget ? (
              <Link
                onClick={() =>
                  onNavigate({ typeKey: ownerTarget, namespace, objectName: owner })
                }
              >
                {owner}
              </Link>
            ) : (
              owner
            )}
          </Property>
        )}
        <Property label="Status">
          <span style={{ color: statusColor(row.fields['status'] ?? '') }}>
            {row.fields['status'] ?? '—'}
          </span>
        </Property>
        {typeof spec.nodeName === 'string' && (
          <Property label="Node">
            <Link
              onClick={() => onNavigate({ typeKey: 'nodes', namespace: '', objectName: String(spec.nodeName) })}
            >
              {String(spec.nodeName)}
            </Link>
          </Property>
        )}
        {typeof status.podIP === 'string' && <Property label="Pod IP">{status.podIP}</Property>}
        {typeof spec.serviceAccountName === 'string' && (
          <Property label="Service Account">{spec.serviceAccountName}</Property>
        )}
        {typeof status.qosClass === 'string' && <Property label="QoS Class">{status.qosClass}</Property>}
        <Property label="Conditions">
          <Conditions status={status} />
        </Property>
        <Property label="Tolerations">
          {Array.isArray(spec.tolerations) ? spec.tolerations.length : 0}
        </Property>
      </Section>

      {volumes.length > 0 && (
        <Section title={`Pod Volumes (${volumes.length})`}>
          {volumes.map((volume) => (
            <Property key={volume.name} label={volume.type || 'Volume'}>
              {volume.name}
              {volume.source ? ` · ${volume.source}` : ''}
            </Property>
          ))}
        </Section>
      )}

      {/* Init containers keep a section of their own (ADR-134): they ran and exited
          before anything else started, and reading them among the ones still running
          invites treating the two as the same thing. */}
      {[false, true].map((init) => {
        const group = containers.filter((container) => container.init === init)
        if (group.length === 0) return null

        return (
          <Section key={String(init)} title={`${init ? 'Init Containers' : 'Containers'} (${group.length})`}>
            {group.map((container) => (
              <Container
                key={container.name}
                clusterId={clusterId}
                typeKey={typeKey}
                namespace={namespace}
                podName={row.name}
                container={container}
                status={status}
                metric={metric}
                usage={metrics.data?.usage?.containers?.[container.name]}
              />
            ))}
          </Section>
        )
      })}

      <Section title="Events">
        {about.length === 0 ? (
          <Quiet>{events.isPending ? 'Reading events…' : 'No events found.'}</Quiet>
        ) : (
          <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
            <thead>
              <tr style={{ color: 'var(--text-muted)' }}>
                <Th>Summary</Th>
                <Th>Count</Th>
                <Th>Age</Th>
              </tr>
            </thead>
            <tbody>
              {about.slice(0, 30).map((event) => (
                <tr key={`${event.namespace}/${event.name}`}>
                  <Td>
                    <span
                      className="mr-1.5 inline-block h-3 w-0.5 align-middle"
                      style={{
                        backgroundColor:
                          event.fields['type'] === 'Warning' ? 'var(--status-warn)' : 'var(--border-strong)',
                      }}
                    />
                    {event.fields['message']}
                  </Td>
                  <Td mono>{event.fields['count'] ?? '—'}</Td>
                  <Td mono>{event.fields['lastSeen'] ? formatAge(event.fields['lastSeen']) : event.age}</Td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>
    </div>
  )
}

/** One container: what it is, what it is doing, and what it is allowed. */
function Container({
  clusterId,
  typeKey,
  namespace,
  podName,
  container,
  status,
  metric,
  usage,
}: {
  clusterId: string
  typeKey: string
  namespace: string
  podName: string
  container: ContainerSpec
  status: Record<string, unknown>
  metric: 'cpu' | 'memory'
  usage: ContainerUsage | undefined
}) {
  const statuses = [
    ...(Array.isArray(status.containerStatuses) ? (status.containerStatuses as Record<string, unknown>[]) : []),
    ...(Array.isArray(status.initContainerStatuses)
      ? (status.initContainerStatuses as Record<string, unknown>[])
      : []),
  ]
  const own = statuses.find((entry) => entry.name === container.name)
  const state = own?.state as Record<string, unknown> | undefined
  const running = state?.running !== undefined
  const label = running ? (own?.ready ? 'running, ready' : 'running, not ready') : describe(state)

  const points = (metric === 'cpu' ? usage?.cpuCores : usage?.memoryBytes) ?? []
  const origin = registryOf(container.image)
  const pulled = pulledFrom(container.image, text(own?.imageID))

  return (
    <div className="border-t" style={{ borderColor: 'var(--border-subtle)' }}>
      <h4 className="flex items-center gap-2 px-3 pb-1 pt-2.5">
        <span
          aria-hidden="true"
          className="h-2 w-2 shrink-0"
          style={{ borderRadius: '1px', backgroundColor: statusColor(running ? 'Running' : label) }}
        />
        <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
          {container.name}
        </span>
        {container.init && (
          <span
            className="px-1 py-0.5 font-mono uppercase"
            style={{
              fontSize: 'var(--text-micro)',
              borderRadius: 'var(--radius-sharp)',
              backgroundColor: 'var(--bg-raised)',
              color: 'var(--text-muted)',
            }}
          >
            init
          </span>
        )}
      </h4>

      {points.length >= 2 && (
        <div className="px-3 pb-2">
          <AreaChart
            series={[
              {
                name: metric === 'cpu' ? 'CPU usage' : 'Memory usage',
                colour: metric === 'cpu' ? SERIES_COLOURS[0]! : SERIES_COLOURS[7]!,
                points,
              },
            ]}
            height={110}
            render={metric === 'cpu' ? (value) => value.toFixed(3) : formatBytes}
            ticks={13}
            values
          />
        </div>
      )}

      <Property label="Status">
        <span style={{ color: statusColor(running ? 'Running' : label) }}>{label}</span>
      </Property>
      <Property label="Image">
        <span className="flex flex-col gap-0.5">
          <span className="break-all">{container.image}</span>
          <span style={{ color: 'var(--text-muted)' }}>
            {origin && (
              <>
                registry <span style={{ color: 'var(--text-secondary)' }}>{origin.host}</span>
                {origin.implicit && ' (implicit)'}
              </>
            )}
            {container.pullPolicy && ` · pull ${container.pullPolicy}`}
          </span>
          {pulled && (
            <span className="break-all" style={{ color: 'var(--text-muted)' }}>
              pulled <span style={{ color: 'var(--text-secondary)' }}>{pulled}</span>
            </span>
          )}
        </span>
      </Property>
      {container.ports.length > 0 && (
        <Property label="Ports">
          <span className="flex flex-col gap-1">
            {container.ports.map((port) => (
              <ForwardablePort
                key={`${port.port}/${port.protocol}`}
                clusterId={clusterId}
                typeKey={typeKey}
                namespace={namespace}
                name={podName}
                port={port.port}
                protocol={port.protocol}
                label={port.name}
              />
            ))}
          </span>
        </Property>
      )}
      <Property label="Environment">{container.env > 0 ? `${container.env} variables` : '—'}</Property>
      {container.mounts.length > 0 && (
        <Property label="Mounts">
          <span className="flex flex-col gap-0.5">
            {container.mounts.map((mount) => (
              <span key={`${mount.volume}:${mount.path}`} className="break-all">
                {mount.path}
                <span style={{ color: 'var(--text-muted)' }}>
                  {' from '}
                  {mount.volume}
                  {mount.readOnly ? ' (ro)' : ''}
                </span>
              </span>
            ))}
          </span>
        </Property>
      )}
      {container.liveness && <Property label="Liveness">{container.liveness}</Property>}
      {container.readiness && <Property label="Readiness">{container.readiness}</Property>}
      <Property label="Requests">{formatQuantities(container.requests) || 'none'}</Property>
      <Property label="Limits">{formatQuantities(container.limits) || 'none'}</Property>
    </div>
  )
}

function describe(state: Record<string, unknown> | undefined): string {
  const waiting = state?.waiting as Record<string, unknown> | undefined
  const terminated = state?.terminated as Record<string, unknown> | undefined

  if (waiting) return text(waiting.reason) || 'waiting'
  if (terminated) {
    const reason = text(terminated.reason) || 'terminated'
    return typeof terminated.exitCode === 'number' ? `${reason}, exit ${terminated.exitCode}` : reason
  }
  return 'unknown'
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

/** The pod's own usage, above the containers that make it up. */
function Usage({
  title,
  metric,
  metrics,
}: {
  title: string
  metric: 'cpu' | 'memory'
  metrics: PodMetrics | undefined
}) {
  const points = (metric === 'cpu' ? metrics?.usage?.cpuCores : metrics?.usage?.memoryBytes) ?? []

  return (
    <Section title={title}>
      {points.length < 2 ? (
        <Quiet>
          {metrics && !metrics.configured
            ? 'Metrics come from Prometheus; this cluster has none configured.'
            : `No ${metric} history for this pod yet.`}
        </Quiet>
      ) : (
        <div className="px-3 py-2">
          <AreaChart
            series={[
              {
                name: metric === 'cpu' ? 'CPU usage' : 'Memory usage',
                colour: metric === 'cpu' ? SERIES_COLOURS[0]! : SERIES_COLOURS[7]!,
                points,
              },
            ]}
            height={140}
            render={metric === 'cpu' ? (value) => value.toFixed(3) : formatBytes}
            ticks={13}
            values
          />
        </div>
      )}
    </Section>
  )
}

function Conditions({ status }: { status: Record<string, unknown> }) {
  const conditions = Array.isArray(status.conditions)
    ? (status.conditions as Record<string, string>[])
    : []
  const met = conditions.filter((condition) => condition.status === 'True')
  if (met.length === 0) return <>—</>

  return (
    <span className="flex flex-wrap gap-1">
      {met.map((condition) => (
        <span
          key={condition.type}
          title={condition.message}
          className="px-1.5 py-0.5"
          style={{
            borderRadius: 'var(--radius-sharp)',
            border: '1px solid var(--status-ok)',
            color: 'var(--status-ok)',
          }}
        >
          {condition.type}
        </span>
      ))}
    </span>
  )
}

/** A count that opens into what it counted. */
function Chips({ label, values }: { label: string; values: unknown }) {
  const [open, setOpen] = useState(false)
  const entries = values && typeof values === 'object' ? Object.entries(values as Record<string, string>) : []
  const summary = `${entries.length} ${label === 'Labels' ? 'Label' : 'Annotation'}${entries.length === 1 ? '' : 's'}`

  if (entries.length === 0) return <Property label={label}>{summary}</Property>

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-expanded={false}
        className="grid w-full grid-cols-[9rem_1fr] items-baseline gap-2 border-t px-3 py-1.5 text-left transition-colors hover:bg-[var(--bg-hover)]"
        style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
      >
        <span style={{ color: 'var(--text-muted)' }}>{label} ›</span>
        <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
          {summary}
        </span>
      </button>
    )
  }

  return (
    <Property label={label}>
      <button type="button" onClick={() => setOpen(false)} className="flex flex-wrap gap-1 text-left">
        {entries.map(([key, value]) => (
          <span
            key={key}
            title={`${key}=${value}`}
            className="max-w-full truncate px-1.5 py-0.5"
            style={{
              borderRadius: 'var(--radius-sharp)',
              backgroundColor: 'var(--bg-raised)',
              color: 'var(--text-secondary)',
            }}
          >
            <span style={{ color: 'var(--accent)' }}>{key}</span>
            {value ? `=${value}` : ''}
          </span>
        ))}
      </button>
    </Property>
  )
}

function Link({ children, onClick }: { children: React.ReactNode; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="truncate text-left hover:underline" style={{ color: 'var(--status-info)' }}>
      {children}
    </button>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-b" style={{ borderColor: 'var(--border-subtle)' }}>
      <h3
        className="px-3 pb-1 pt-3 font-semibold"
        style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}
      >
        {title}
      </h3>
      {children}
    </section>
  )
}

function Property({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div
      className="grid grid-cols-[9rem_1fr] items-baseline gap-2 border-t px-3 py-1.5"
      style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
    >
      <dt style={{ color: 'var(--text-muted)' }}>{label}</dt>
      <dd className="min-w-0 break-words font-mono" style={{ color: 'var(--text-primary)' }}>
        {children}
      </dd>
    </div>
  )
}

function Th({ children }: { children?: React.ReactNode }) {
  return (
    <th className="px-3 py-1.5 text-left" style={{ fontWeight: 600 }}>
      {children}
    </th>
  )
}

function Td({ children, mono }: { children?: React.ReactNode; mono?: boolean }) {
  return (
    <td className={`px-3 py-1 ${mono ? 'font-mono' : ''}`} style={{ color: 'var(--text-secondary)' }}>
      {children}
    </td>
  )
}

function Quiet({ children }: { children: React.ReactNode }) {
  return (
    <p className="px-3 py-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
      {children}
    </p>
  )
}
