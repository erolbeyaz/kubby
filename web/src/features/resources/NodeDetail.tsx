import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { api, type ClusterMetrics, type ResourceRow } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

import { AreaChart, SERIES_COLOURS, formatBytes } from '@/features/metrics/charts'

import { statusColor } from './statusColor'
import type { NavigationTarget } from './ResourceTable'

interface NodeDetailProps {
  clusterId: string
  row: ResourceRow
  object: Record<string, unknown>
  /** Which measurement the chart draws. Chosen on the tab row beside YAML. */
  metric: 'cpu' | 'memory'
  onNavigate: (target: NavigationTarget) => void
}

/**
 * A machine, read the way someone asks about one.
 *
 * The order is the order the questions come in: how hard is it working, what is it, how
 * much has it got, how much of that can be handed out, what is running on it, and what
 * has happened to it lately. Capacity and allocatable sit next to each other because the
 * difference between them is what the kubelet reserved, and that difference is the whole
 * reason both are shown.
 */
export function NodeDetail({ clusterId, row, object, metric, onNavigate }: NodeDetailProps) {
  const status = (object.status ?? {}) as Record<string, unknown>
  const metadata = (object.metadata ?? {}) as Record<string, unknown>
  const info = (status.nodeInfo ?? {}) as Record<string, string>

  const metrics = useQuery({
    queryKey: ['cluster-metrics', clusterId, '1h'],
    queryFn: ({ signal }) => api.clusterMetrics(clusterId, '1h', signal),
    staleTime: 30_000,
  })

  // Every pod, narrowed to this machine. The list already projects the node each pod
  // landed on, so this is a filter rather than a second kind of question.
  const pods = useQuery({
    queryKey: ['resources', clusterId, 'pods', 'all'],
    queryFn: ({ signal }) => api.resources(clusterId, 'pods', {}, signal),
    staleTime: 15_000,
  })

  const events = useQuery({
    queryKey: ['resources', clusterId, 'events', row.name],
    queryFn: ({ signal }) => api.resources(clusterId, 'events', { search: row.name }, signal),
    staleTime: 15_000,
  })

  const here = (pods.data?.rows ?? []).filter((pod) => pod.fields['node'] === row.name)
  const about = (events.data?.rows ?? []).filter((event) =>
    (event.fields['involvedObject'] ?? '').includes(row.name),
  )

  return (
    <div className="flex flex-col">
      <Metrics nodeName={row.name} metric={metric} metrics={metrics.data} loading={metrics.isPending} />

      <Section title="Properties">
        <Property label="Created">
          {row.createdAt ? `${formatAge(row.createdAt)} ago · ${formatAbsolute(row.createdAt)}` : '—'}
        </Property>
        <Property label="Name">{row.name}</Property>
        <Expandable label="Labels" values={metadata.labels} noun="Label" />
        <Expandable label="Annotations" values={metadata.annotations} noun="Annotation" />
        {Array.isArray(metadata.finalizers) && metadata.finalizers.length > 0 && (
          <Property label="Finalizers">
            <span className="flex flex-wrap gap-1">
              {(metadata.finalizers as string[]).map((finalizer) => (
                <span
                  key={finalizer}
                  className="px-1.5 py-0.5"
                  style={{
                    borderRadius: 'var(--radius-sharp)',
                    backgroundColor: 'var(--bg-raised)',
                    color: 'var(--text-secondary)',
                  }}
                >
                  {finalizer}
                </span>
              ))}
            </span>
          </Property>
        )}
        <Property label="Addresses">
          <span className="flex flex-col gap-0.5">
            {(Array.isArray(status.addresses) ? (status.addresses as Record<string, string>[]) : []).map(
              (address) => (
                <span key={`${address.type}:${address.address}`}>
                  {address.type}: {address.address}
                </span>
              ),
            )}
          </span>
        </Property>
        <Property label="OS">
          {info.operatingSystem} ({info.architecture})
        </Property>
        <Property label="OS Image">{info.osImage}</Property>
        <Property label="Kernel version">{info.kernelVersion}</Property>
        <Property label="Container runtime">{info.containerRuntimeVersion}</Property>
        <Property label="Kubelet version">{info.kubeletVersion}</Property>
        <Property label="Conditions">
          <Conditions status={status} />
        </Property>
      </Section>

      <Resources title="Capacity" values={status.capacity} />
      {/* Beside capacity rather than anywhere else: the gap between the two is what the
          kubelet reserved for itself, and that gap is why both are worth showing. */}
      <Resources title="Allocatable" values={status.allocatable} />

      <Section title={`Pods (${here.length})`}>
        {pods.isPending ? (
          <Quiet>Reading the pods on this node…</Quiet>
        ) : here.length === 0 ? (
          <Quiet>Nothing is scheduled here.</Quiet>
        ) : (
          <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
            <thead>
              <tr style={{ color: 'var(--text-muted)' }}>
                <Th>Name</Th>
                <Th>Namespace</Th>
                <Th>Ready</Th>
                <Th>CPU</Th>
                <Th>Memory</Th>
                <Th>Status</Th>
              </tr>
            </thead>
            <tbody>
              {here.map((pod) => (
                <tr key={`${pod.namespace}/${pod.name}`} className="hover:bg-[var(--bg-hover)]">
                  <Td>
                    <button
                      type="button"
                      onClick={() =>
                        onNavigate({
                          typeKey: 'pods',
                          namespace: pod.namespace ?? '',
                          objectName: pod.name,
                        })
                      }
                      className="max-w-[14rem] truncate text-left font-mono hover:underline"
                      style={{ color: 'var(--status-info)' }}
                    >
                      {pod.name}
                    </button>
                  </Td>
                  <Td>{pod.namespace}</Td>
                  <Td mono>{pod.fields['ready'] ?? '—'}</Td>
                  <Td mono>{pod.fields['cpu'] ?? '—'}</Td>
                  <Td mono>{pod.fields['memory'] ?? '—'}</Td>
                  <Td>
                    <span style={{ color: statusColor(pod.fields['status'] ?? '') }}>
                      {pod.fields['status']}
                    </span>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>

      <Section title="Events">
        {events.isPending ? (
          <Quiet>Reading events…</Quiet>
        ) : about.length === 0 ? (
          <Quiet>No events found.</Quiet>
        ) : (
          <div className="flex flex-col gap-1.5 px-3 py-2">
            {about.slice(0, 20).map((event) => (
              <div key={`${event.namespace}/${event.name}`} className="flex flex-col">
                <span
                  style={{
                    fontSize: 'var(--text-micro)',
                    color: event.fields['type'] === 'Warning' ? 'var(--status-warn)' : 'var(--text-muted)',
                  }}
                >
                  {event.fields['reason']} · {event.fields['lastSeen'] ? formatAge(event.fields['lastSeen']) : ''}
                </span>
                <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>
                  {event.fields['message']}
                </span>
              </div>
            ))}
          </div>
        )}
      </Section>
    </div>
  )
}

/**
 * What the machine has been doing.
 *
 * One node's line out of the fleet-wide series, because the question here is about this
 * machine and a chart of every machine answers a different one.
 */
function Metrics({
  nodeName,
  metric,
  metrics,
  loading,
}: {
  nodeName: string
  metric: 'cpu' | 'memory'
  metrics: ClusterMetrics | undefined
  loading: boolean
}) {
  const trends = metrics?.health?.trends
  // Cores and bytes as the kubelet reports them, not a share of the host. The
  // percentage series beside these is read from node-exporter, which measures the
  // machine — right on bare metal, wrong wherever a node is a container, where every
  // node in a cluster reports the same figure because they share one /proc.
  const all = (metric === 'cpu' ? trends?.nodeCpuCoresOverTime : trends?.nodeMemoryBytesOverTime) ?? []
  // Prometheus names a node by its scrape target, which is the node's name in most
  // deployments and an address with a port in some; matching either way is cheaper than
  // being right only sometimes.
  const mine = all.find(
    (series) => series.name === nodeName || series.name.startsWith(`${nodeName}:`),
  )

  return (
    <Section title={metric === 'cpu' ? 'CPU' : 'Memory'}>
      {loading ? (
        <Quiet>Reading metrics…</Quiet>
      ) : !mine || (mine.points ?? []).length < 2 ? (
        <Quiet>
          No {metric} history for this node. Metrics come from Prometheus; a cluster without
          one shows nothing here.
        </Quiet>
      ) : (
        <div className="px-3 py-2">
          <AreaChart
            series={[
              {
                name: metric === 'cpu' ? 'CPU usage' : 'Memory usage',
                // Two colours rather than one, because the two charts sit in the same
                // place and only the legend would otherwise say which is on screen.
                colour: metric === 'cpu' ? SERIES_COLOURS[0]! : SERIES_COLOURS[7]!,
                points: mine.points ?? [],
              },
            ]}
            height={140}
            // Cores and bytes, not a share of them. "48%" says how full the machine is
            // and nothing about whether that is a lot; a pod asks for millicores and
            // mebibytes, and those are the numbers the reader is comparing against.
            render={metric === 'cpu' ? (value) => value.toFixed(3) : formatBytes}
            // A chart with the panel's width to itself can carry a readable clock rather
            // than only its two ends: thirteen stops over an hour lands on five minutes.
            ticks={13}
            values
          />
        </div>
      )}
    </Section>
  )
}

/** Capacity and allocatable, which are the same shape and read as a pair. */
function Resources({ title, values }: { title: string; values: unknown }) {
  const amounts = (values ?? {}) as Record<string, string>
  if (Object.keys(amounts).length === 0) return null

  const columns: [string, string][] = [
    ['CPU', amounts.cpu ?? '—'],
    ['Memory', humanise(amounts.memory)],
    ['Ephemeral Storage', humanise(amounts['ephemeral-storage'])],
    ['Hugepages-1Gi', amounts['hugepages-1Gi'] ?? '0'],
    ['Hugepages-2Mi', amounts['hugepages-2Mi'] ?? '0'],
    ['Pods', amounts.pods ?? '—'],
  ]

  return (
    <Section title={title}>
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr style={{ color: 'var(--text-muted)' }}>
            {columns.map(([label]) => (
              <Th key={label}>{label}</Th>
            ))}
          </tr>
        </thead>
        <tbody>
          <tr>
            {columns.map(([label, value]) => (
              <Td key={label} mono>
                {value}
              </Td>
            ))}
          </tr>
        </tbody>
      </table>
    </Section>
  )
}

function Conditions({ status }: { status: Record<string, unknown> }) {
  const conditions = Array.isArray(status.conditions)
    ? (status.conditions as Record<string, string>[])
    : []

  // Only what is true. A node raises every condition it knows about and sets most of
  // them to False; listing those is a wall of things that are fine.
  const raised = conditions.filter((condition) => condition.status === 'True')
  if (raised.length === 0) return <>—</>

  return (
    <span className="flex flex-wrap gap-1">
      {raised.map((condition) => {
        const good = condition.type === 'Ready'
        return (
          <span
            key={condition.type}
            title={condition.message}
            className="px-1.5 py-0.5"
            style={{
              borderRadius: 'var(--radius-sharp)',
              border: `1px solid ${good ? 'var(--status-ok)' : 'var(--status-warn)'}`,
              color: good ? 'var(--status-ok)' : 'var(--status-warn)',
            }}
          >
            {condition.type}
          </span>
        )
      })}
    </span>
  )
}

/**
 * A count that opens into the thing it counted.
 *
 * A node carries a dozen annotations and half of them are a serialised object; printing
 * them costs the whole panel, and hiding them entirely means the one that matters cannot
 * be found. So the row says how many and opens when asked.
 */
function Expandable({ label, values, noun }: { label: string; values: unknown; noun: string }) {
  const [open, setOpen] = useState(false)

  const entries =
    values && typeof values === 'object' ? Object.entries(values as Record<string, string>) : []
  const summary = `${entries.length} ${noun}${entries.length === 1 ? '' : 's'}`

  if (entries.length === 0) {
    return <Property label={label}>{summary}</Property>
  }

  const chevron = (
    <span aria-hidden="true" className="inline-block" style={{ transform: open ? 'rotate(90deg)' : undefined }}>
      ›
    </span>
  )

  // Collapsed, the whole row opens it; there is one thing to press and it is the size of
  // the row. Open, only the label closes it again, because the chips beside it are the
  // point and clicking one should not put them away.
  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-expanded={false}
        className="grid w-full grid-cols-[9rem_1fr] items-baseline gap-2 border-t px-3 py-1.5 text-left transition-colors hover:bg-[var(--bg-hover)]"
        style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
      >
        <span className="flex items-center gap-1" style={{ color: 'var(--text-muted)' }}>
          {label}
          {chevron}
        </span>
        <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
          {summary}
        </span>
      </button>
    )
  }

  return (
    <div
      className="grid grid-cols-[9rem_1fr] items-baseline gap-2 border-t px-3 py-1.5"
      style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
    >
      <button
        type="button"
        onClick={() => setOpen(false)}
        aria-expanded
        className="flex items-center gap-1 text-left transition-colors hover:text-[var(--text-secondary)]"
        style={{ color: 'var(--text-muted)' }}
      >
        {label}
        {chevron}
      </button>

      <dd className="flex min-w-0 flex-wrap gap-1">
        {entries.map(([key, value]) => (
          <span
            key={key}
            title={`${key}=${value}`}
            className="max-w-full truncate px-1.5 py-0.5 font-mono"
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
      </dd>
    </div>
  )
}

/** Kubernetes writes memory as a quantity; a reader wants it in the units they think in. */
function humanise(quantity: string | undefined): string {
  const bytes = quantityBytes(quantity)
  return bytes > 0 ? formatBytes(bytes) : (quantity ?? '—')
}

function quantityBytes(quantity: string | undefined): number {
  if (!quantity) return 0

  const match = /^(\d+)(Ki|Mi|Gi|Ti)?$/.exec(quantity)
  if (!match) return 0

  const scale: Record<string, number> = { Ki: 1024, Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4 }
  return Number(match[1]) * (match[2] ? (scale[match[2]] ?? 1) : 1)
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
    <th className="px-3 py-1.5 text-left font-normal" style={{ fontWeight: 600 }}>
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
