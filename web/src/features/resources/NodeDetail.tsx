import { useQuery } from '@tanstack/react-query'

import { api, type ClusterMetrics, type ResourceRow } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

import { AreaChart, SERIES_COLOURS, formatBytes } from '@/features/metrics/charts'

import { statusColor } from './statusColor'
import type { NavigationTarget } from './ResourceTable'

interface NodeDetailProps {
  clusterId: string
  row: ResourceRow
  object: Record<string, unknown>
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
export function NodeDetail({ clusterId, row, object, onNavigate }: NodeDetailProps) {
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
      <Metrics nodeName={row.name} metrics={metrics.data} loading={metrics.isPending} />

      <Section title="Properties">
        <Property label="Created">
          {row.createdAt ? `${formatAge(row.createdAt)} ago · ${formatAbsolute(row.createdAt)}` : '—'}
        </Property>
        <Property label="Name">{row.name}</Property>
        <Property label="Labels">{count(metadata.labels, 'Label')}</Property>
        <Property label="Annotations">{count(metadata.annotations, 'Annotation')}</Property>
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
  metrics,
  loading,
}: {
  nodeName: string
  metrics: ClusterMetrics | undefined
  loading: boolean
}) {
  const all = metrics?.health?.trends?.cpuByNodeOverTime ?? []
  // Prometheus names a node by its scrape target, which is the node's name in most
  // deployments and an address with a port in some; matching either way is cheaper than
  // being right only sometimes.
  const mine = all.find(
    (series) => series.name === nodeName || series.name.startsWith(`${nodeName}:`),
  )

  return (
    <Section title="Metrics">
      {loading ? (
        <Quiet>Reading metrics…</Quiet>
      ) : !mine || (mine.points ?? []).length < 2 ? (
        <Quiet>
          No history for this node. Metrics come from Prometheus; a cluster without one shows
          nothing here.
        </Quiet>
      ) : (
        <div className="px-3 py-2">
          <AreaChart
            series={[{ name: 'CPU usage', colour: SERIES_COLOURS[0]!, points: mine.points ?? [] }]}
            unit="%"
            height={140}
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
            className="px-1.5 py-0.5 font-semibold"
            style={{
              borderRadius: 'var(--radius-sharp)',
              backgroundColor: good ? 'var(--status-ok)' : 'var(--status-warn)',
              color: 'var(--text-inverse)',
            }}
          >
            {condition.type}
          </span>
        )
      })}
    </span>
  )
}

function count(value: unknown, noun: string): string {
  const total = value && typeof value === 'object' ? Object.keys(value).length : 0
  return `${total} ${noun}${total === 1 ? '' : 's'}`
}

/** Kubernetes writes memory as a quantity; a reader wants it in the units they think in. */
function humanise(quantity: string | undefined): string {
  if (!quantity) return '—'

  const match = /^(\d+)(Ki|Mi|Gi|Ti)?$/.exec(quantity)
  if (!match) return quantity

  const scale: Record<string, number> = { Ki: 1024, Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4 }
  const bytes = Number(match[1]) * (match[2] ? (scale[match[2]] ?? 1) : 1)
  return formatBytes(bytes)
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
