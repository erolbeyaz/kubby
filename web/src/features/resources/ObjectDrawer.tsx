import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { CopyButton } from '@/components/CopyButton'
import { LazyYamlViewer, warmYamlViewer } from '@/components/LazyYamlViewer'
import { ApiError, api, type ResourceRow } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'
import { toYaml } from '@/lib/yaml'

import { availableActionsFor } from './actions'
import { containersOf, formatQuantities, podSpecOf, pulledFrom, registryOf, volumesOf } from './containers'
import { ForwardablePort } from './ForwardablePort'
import { NodeDetail } from './NodeDetail'
import { Relations } from './Relations'
import { RestartBadge } from './RestartBadge'
import { SecretKeys } from './SecretKeys'

import { statusColor, typeKeyForKind } from './statusColor'
import type { NavigationTarget } from './ResourceTable'

interface ObjectDrawerProps {
  clusterId: string
  typeKey: string
  kind: string
  row: ResourceRow
  onClose: () => void
  onNavigate: (target: NavigationTarget) => void
  /** Runs one of the object's actions. The panel decides which to offer, not what they do. */
  onAction: (id: string) => void
}

type Pane = 'summary' | 'yaml'

/** Which measurement a node's chart is drawing. */
type Metric = 'cpu' | 'memory'

/**
 * A detail panel beside the list rather than instead of it.
 *
 * Inspecting a pod is usually a step in scanning a list, not a departure from it:
 * keeping the list on screen means the next row is one click away instead of a
 * navigation round trip.
 */
export function ObjectDrawer({ clusterId, typeKey, kind, row, onClose, onNavigate, onAction }: ObjectDrawerProps) {
  const [pane, setPane] = useState<Pane>('summary')
  const [metric, setMetric] = useState<Metric>('cpu')

  // Opening an object is a good predictor of reading its YAML, so the editor is fetched
  // now rather than when the tab is clicked and the wait would be visible.
  useEffect(warmYamlViewer, [])

  const object = useQuery({
    queryKey: ['object', clusterId, typeKey, row.namespace ?? '', row.name],
    queryFn: ({ signal }) =>
      api.resourceObject(
        clusterId,
        typeKey,
        { name: row.name, ...(row.namespace ? { namespace: row.namespace } : {}) },
        signal,
      ),
  })

  const error = object.error instanceof ApiError ? object.error : null
  const data = object.data as KubeObject | undefined

  return (
    <aside
      aria-label={`${kind} ${row.name}`}
      className="flex h-full flex-col border-l"
      style={{ borderColor: 'var(--border-default)', backgroundColor: 'var(--bg-surface)' }}
    >
      <header
        className="flex h-11 shrink-0 items-center gap-2 border-b px-3"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <span
          className="shrink-0 px-1.5 py-0.5 font-mono uppercase"
          style={{
            fontSize: 'var(--text-micro)',
            borderRadius: 'var(--radius-sharp)',
            backgroundColor: 'var(--accent-muted)',
            color: 'var(--accent)',
          }}
        >
          {kind}
        </span>
        <h2 className="min-w-0 truncate" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
          {row.name}
        </h2>

        {/* The name is the thing most often carried out of this panel — into a terminal,
            a ticket, a search — and it is truncated here, so selecting it by hand gets
            half of it. */}
        <span className="mr-auto shrink-0">
          <CopyButton value={row.name} label="Copy" iconOnly />
        </span>

        <ActionIcons kind={kind} onAction={onAction} />

        <button
          type="button"
          onClick={onClose}
          aria-label="Close details"
          className="flex h-7 w-7 items-center justify-center transition-colors hover:bg-[var(--bg-hover)]"
          style={{ borderRadius: 'var(--radius-sharp)', color: 'var(--text-muted)' }}
        >
          <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
            <path d="M2 2 L10 10 M10 2 L2 10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
      </header>

      <div
        role="tablist"
        className="flex h-8 shrink-0 items-stretch border-b"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <PaneTab label="Summary" active={pane === 'summary'} onClick={() => setPane('summary')} />
        <PaneTab label="YAML" active={pane === 'yaml'} onClick={() => setPane('yaml')} />

        {/* Beside the tabs because they change what the panel is showing, the same as a
            tab does. Only a node has a chart to switch. */}
        {kind === 'Node' && pane === 'summary' && (
          <span className="flex items-center gap-0.5 pl-2">
            <MetricTab
              label="CPU"
              active={metric === 'cpu'}
              onClick={() => setMetric('cpu')}
              path="M5.5 5.5h5v5h-5zM3 6.5h2M3 9.5h2M11 6.5h2M11 9.5h2M6.5 3v2M9.5 3v2M6.5 11v2M9.5 11v2"
            />
            <MetricTab
              label="Memory"
              active={metric === 'memory'}
              onClick={() => setMetric('memory')}
              path="M2.5 5.5h11v5h-11zM5 10.5v2M8 10.5v2M11 10.5v2M5 7v2M8 7v2M11 7v2"
            />
          </span>
        )}

        {/* On the tab row rather than floating over the text: it acts on what the YAML
            tab shows, and over the content it covered the first line of every manifest. */}
        {pane === 'yaml' && data && (
          <span className="ml-auto flex items-center pr-1.5">
            <CopyButton value={toYaml(data)} label="Copy YAML" />
          </span>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {error && (
          <div className="p-3">
            <Callout tone="error" title="Could not load" requestId={error.requestId}>
              {error.message}
            </Callout>
          </div>
        )}

        {/* The summary draws from the row the list already holds, so it is on screen the
            moment the panel opens; the fetched object fills in the rest behind it. */}
        {/* A node is read differently from anything else here — how hard is it working,
            what is it made of, how much can it hand out — so it has a panel of its own
            rather than the generic properties list bent towards it. */}
        {!error && pane === 'summary' && kind === 'Node' && (
          <NodeDetail
            clusterId={clusterId}
            row={row}
            object={data ?? {}}
            metric={metric}
            onNavigate={onNavigate}
          />
        )}

        {!error && pane === 'summary' && kind !== 'Node' && (
          <>
            <Summary
              clusterId={clusterId}
              typeKey={typeKey}
              object={data ?? {}}
              row={row}
              kind={kind}
              loading={object.isLoading}
              onNavigate={onNavigate}
            />

            {kind === 'Secret' && (
              <Group title="Data">
                <SecretKeys clusterId={clusterId} namespace={row.namespace ?? ''} name={row.name} />
              </Group>
            )}

            <Group title="Related">
              <Relations
                clusterId={clusterId}
                typeKey={typeKey}
                namespace={row.namespace ?? ''}
                name={row.name}
                onNavigate={onNavigate}
              />
            </Group>

            {kind === 'Pod' && (
              <Group title="Restarts">
                <RestartBadge clusterId={clusterId} namespace={row.namespace ?? ''} pod={row.name} detailed />
              </Group>
            )}
          </>
        )}

        {pane === 'yaml' &&
          (data ? (
            <div className="h-full">
              <LazyYamlViewer value={toYaml(data)} ariaLabel={`${kind} ${row.name} as YAML`} hideCopy />
            </div>
          ) : (
            <DrawerSkeleton />
          ))}
      </div>
    </aside>
  )
}

// ---------------------------------------------------------------- summary

interface KubeObject {
  metadata?: Record<string, unknown>
  spec?: Record<string, unknown>
  status?: Record<string, unknown>
  [key: string]: unknown
}

function Summary({
  clusterId,
  typeKey,
  object,
  row,
  kind,
  loading,
  onNavigate,
}: {
  clusterId: string
  typeKey: string
  object: KubeObject
  row: ResourceRow
  kind: string
  loading: boolean
  onNavigate: (target: NavigationTarget) => void
}) {
  const metadata = object.metadata ?? {}
  const spec = object.spec ?? {}
  const status = object.status ?? {}
  const labels = (metadata.labels ?? {}) as Record<string, string>
  // The row already carries most of what the properties list shows; the object only
  // adds what a list does not project.
  const created =
    typeof metadata.creationTimestamp === 'string' ? metadata.creationTimestamp : row.createdAt

  const ownerName = row.fields['controlledBy'] ?? ''
  const ownerKind = row.fields['controlledByKind'] ?? ''
  const ownerTarget = ownerKind ? typeKeyForKind(ownerKind) : null

  const conditions = Array.isArray(status.conditions) ? (status.conditions as Record<string, unknown>[]) : []

  return (
    <div className="flex flex-col">
      <Group title="Properties">
        <Property label="Created">
          {created ? `${formatAge(created)} ago · ${formatAbsolute(created)}` : '—'}
        </Property>
        {row.namespace && (
          <Property label="Namespace">
            <LinkValue value={row.namespace} onClick={() => onNavigate({ namespace: row.namespace ?? '' })} />
          </Property>
        )}
        {ownerName && (
          <Property label="Controlled By">
            <span style={{ color: 'var(--text-muted)' }}>{ownerKind} </span>
            <LinkValue
              value={ownerName}
              onClick={
                ownerTarget
                  ? () =>
                      onNavigate({
                        typeKey: ownerTarget,
                        namespace: row.namespace ?? '',
                        objectName: ownerName,
                      })
                  : undefined
              }
            />
          </Property>
        )}
        {row.fields['status'] && (
          <Property label="Status">
            <span style={{ color: statusColor(row.fields['status']) }}>{row.fields['status']}</span>
          </Property>
        )}
        {row.fields['node'] && (
          <Property label="Node">
            <LinkValue
              value={row.fields['node']}
              onClick={() => onNavigate({ typeKey: 'nodes', namespace: '', objectName: row.fields['node'] })}
            />
          </Property>
        )}
        {typeof status.podIP === 'string' && <Property label="Pod IP">{status.podIP}</Property>}
        {typeof spec.serviceAccountName === 'string' && (
          <Property label="Service Account">{spec.serviceAccountName}</Property>
        )}
        {typeof status.qosClass === 'string' && <Property label="QoS Class">{status.qosClass}</Property>}
      </Group>

      {Object.keys(labels).length > 0 && (
        <Group title={`Labels (${Object.keys(labels).length})`}>
          <div className="flex flex-wrap gap-1 px-3 py-2">
            {Object.entries(labels).map(([key, value]) => (
              <span
                key={key}
                title={`${key}=${value}`}
                className="max-w-full truncate px-1.5 py-0.5 font-mono"
                style={{
                  fontSize: 'var(--text-micro)',
                  borderRadius: 'var(--radius-sharp)',
                  backgroundColor: 'var(--bg-raised)',
                  color: 'var(--text-secondary)',
                }}
              >
                <span style={{ color: 'var(--accent)' }}>{key}</span>=<span>{value}</span>
              </span>
            ))}
          </div>
        </Group>
      )}

      {conditions.length > 0 && (
        <Group title="Conditions">
          <div className="flex flex-wrap gap-1 px-3 py-2">
            {conditions.map((condition) => {
              const type = text(condition.type)
              const met = text(condition.status) === 'True'
              return (
                <span
                  key={type}
                  title={text(condition.message ?? condition.reason)}
                  className="px-1.5 py-0.5"
                  style={{
                    fontSize: 'var(--text-micro)',
                    borderRadius: 'var(--radius-sharp)',
                    border: `1px solid ${met ? 'var(--status-ok)' : 'var(--border-strong)'}`,
                    color: met ? 'var(--status-ok)' : 'var(--text-muted)',
                  }}
                >
                  {type}
                </span>
              )
            })}
          </div>
        </Group>
      )}

      {loading && Object.keys(object).length === 0 ? (
        <Group title="Containers">
          <div className="px-3 py-2">
            <DrawerSkeleton />
          </div>
        </Group>
      ) : (
        <>
          <Containers
            clusterId={clusterId}
            typeKey={typeKey}
            spec={spec}
            status={status}
            row={row}
            kind={kind}
            init={false}
          />
          {/* Init containers get their own section rather than a badge among the rest:
              they ran and exited before anything else started, and reading them beside
              the containers still running invites treating the two as the same thing. */}
          <Containers
            clusterId={clusterId}
            typeKey={typeKey}
            spec={spec}
            status={status}
            row={row}
            kind={kind}
            init
          />
          <Volumes spec={spec} kind={kind} />
          <ServicePorts clusterId={clusterId} typeKey={typeKey} row={row} kind={kind} spec={spec} />
          <ExternalAddresses row={row} />
        </>
      )}
    </div>
  )
}

/**
 * The containers a resource runs, and what each one is made of.
 *
 * A pod shows what it is running; a workload shows what it will run, read out of the
 * template it stamps out — "which image is this" is asked of a deployment at least as
 * often as of one of its pods.
 */
function Containers({
  clusterId,
  typeKey,
  spec,
  status,
  row,
  kind,
  init,
}: {
  clusterId: string
  typeKey: string
  spec: Record<string, unknown>
  status: Record<string, unknown>
  row: ResourceRow
  kind: string
  init: boolean
}) {
  const containers = containersOf(podSpecOf(kind, spec)).filter((container) => container.init === init)
  if (containers.length === 0) return null

  const isPod = kind === 'Pod'
  const statuses = init ? status.initContainerStatuses : status.containerStatuses
  const containerStatuses = Array.isArray(statuses) ? (statuses as Record<string, unknown>[]) : []

  // Usage is measured for the pod as a whole, so it is divided evenly rather than
  // claimed per container — an honest approximation beats a precise-looking guess.
  const podCpu = parseMilli(row.fields['cpu'] ?? '')
  const podMemory = parseMi(row.fields['memory'] ?? '')

  return (
    <Group title={`${init ? 'Init Containers' : 'Containers'} (${containers.length})`}>
      <div className="flex flex-col gap-3 px-3 py-2">
        {containers.map((container) => {
          const containerStatus = containerStatuses.find((s) => text(s.name) === container.name)
          const state = containerState(containerStatus)

          return (
            <div key={container.name}>
              <div className="mb-1 flex items-center gap-2">
                {isPod && (
                  <span
                    className="h-2 w-2 shrink-0"
                    style={{ borderRadius: '1px', backgroundColor: statusColor(state.label) }}
                  />
                )}
                <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
                  {container.name}
                </span>
                {isPod && state.label && (
                  <span
                    className="ml-auto"
                    style={{ fontSize: 'var(--text-micro)', color: statusColor(state.label) }}
                    title={state.detail}
                  >
                    {state.label}
                  </span>
                )}
              </div>

              <ContainerField label="Image">
                <div className="flex items-start gap-1.5">
                  <span className="min-w-0 flex-1 font-mono break-all" style={{ color: 'var(--text-primary)' }}>
                    {container.image}
                  </span>
                  <CopyButton value={container.image} label="Copy image" />
                </div>
                <ImageOrigin
                  image={container.image}
                  pullPolicy={container.pullPolicy}
                  imageID={isPod ? text(containerStatus?.imageID) : ''}
                />
              </ContainerField>

              {container.ports.length > 0 && (
                <ContainerField label="Ports">
                  <div className="flex flex-col gap-1">
                    {container.ports.map((port) => (
                      <ForwardablePort
                        key={`${port.port}/${port.protocol}`}
                        clusterId={clusterId}
                        typeKey={typeKey}
                        namespace={row.namespace ?? ''}
                        name={row.name}
                        port={port.port}
                        protocol={port.protocol}
                        label={port.name}
                      />
                    ))}
                  </div>
                </ContainerField>
              )}

              {/* Requests and limits, spelled out. The bars below show what is being
                  used against them; the numbers are what was actually asked for, and
                  a container with neither is the one worth noticing. */}
              <ContainerField label="Requests">
                <span className="font-mono">{formatQuantities(container.requests) || 'none'}</span>
              </ContainerField>
              <ContainerField label="Limits">
                <span className="font-mono">{formatQuantities(container.limits) || 'none'}</span>
              </ContainerField>

              {container.mounts.length > 0 && (
                <ContainerField label="Mounts">
                  <div className="flex flex-col gap-0.5 font-mono">
                    {container.mounts.map((mount) => (
                      <span key={`${mount.volume}:${mount.path}`} className="break-all">
                        {mount.path}
                        <span style={{ color: 'var(--text-muted)' }}>
                          {' ← '}
                          {mount.volume}
                          {mount.readOnly && ' (ro)'}
                        </span>
                      </span>
                    ))}
                  </div>
                </ContainerField>
              )}

              {isPod && !init && (
                <div className="mt-1.5">
                  <UsageBar
                    label="CPU"
                    used={podCpu / Math.max(containers.length, 1)}
                    request={parseMilli(container.requests.cpu ?? '')}
                    limit={parseMilli(container.limits.cpu ?? '')}
                    format={(value) => `${Math.round(value)}m`}
                  />
                  <UsageBar
                    label="Memory"
                    used={podMemory / Math.max(containers.length, 1)}
                    request={parseMi(container.requests.memory ?? '')}
                    limit={parseMi(container.limits.memory ?? '')}
                    format={(value) => `${Math.round(value)}Mi`}
                  />
                </div>
              )}
            </div>
          )
        })}
      </div>
    </Group>
  )
}

/**
 * A service's own ports, each one a click away from being reached.
 *
 * A service has no containers, so it never appeared in the block above — and it is the
 * thing most often forwarded, because it is what the rest of the cluster talks to. The
 * forward resolves it to a ready pod behind it rather than to the service address, which
 * is the only way a tunnel can reach it at all.
 */
function ServicePorts({
  clusterId,
  typeKey,
  row,
  kind,
  spec,
}: {
  clusterId: string
  typeKey: string
  row: ResourceRow
  kind: string
  spec: Record<string, unknown>
}) {
  if (kind !== 'Service') return null

  const ports = (Array.isArray(spec.ports) ? (spec.ports as Record<string, unknown>[]) : [])
    .map((port) => ({
      port: typeof port.port === 'number' ? port.port : 0,
      protocol: text(port.protocol) || 'TCP',
      name: text(port.name),
      target: text(port.targetPort) || (typeof port.targetPort === 'number' ? String(port.targetPort) : ''),
    }))
    .filter((port) => port.port > 0)

  if (ports.length === 0) return null

  return (
    <Group title={`Ports (${ports.length})`}>
      <div className="flex flex-col gap-1.5 px-3 py-2">
        {ports.map((port) => (
          <div key={`${port.port}/${port.protocol}`} className="flex items-center gap-2">
            <ForwardablePort
              clusterId={clusterId}
              typeKey={typeKey}
              namespace={row.namespace ?? ''}
              name={row.name}
              port={port.port}
              protocol={port.protocol}
              label={port.name}
            />
            {port.target && (
              <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                → {port.target}
              </span>
            )}
          </div>
        ))}
      </div>
    </Group>
  )
}

/**
 * Where an ingress or a route can actually be reached.
 *
 * The hosts are already projected onto the row with the URL each one resolves to, so
 * this is the same answer the list gives, in the place someone lands after clicking a
 * row rather than beside it.
 */
function ExternalAddresses({ row }: { row: ResourceRow }) {
  const hosts = (row.fields['hosts'] ?? '').split(',').filter(Boolean)
  const urls = (row.fields['hostUrls'] ?? '').split(',')
  if (hosts.length === 0) return null

  return (
    <Group title={`Addresses (${hosts.length})`}>
      <div className="flex flex-col gap-1 px-3 py-2">
        {hosts.map((host, index) => {
          const target = urls[index]?.trim()
          return target ? (
            <a
              key={host}
              href={target}
              target="_blank"
              rel="noreferrer"
              className="truncate font-mono hover:underline"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--status-info)' }}
            >
              {target}
            </a>
          ) : (
            <span
              key={host}
              className="truncate font-mono"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
            >
              {host}
            </span>
          )
        })}
      </div>
    </Group>
  )
}

/** The volumes the pod carries, and what backs each one. */
function Volumes({ spec, kind }: { spec: Record<string, unknown>; kind: string }) {
  const volumes = volumesOf(podSpecOf(kind, spec))
  if (volumes.length === 0) return null

  return (
    <Group title={`Volumes (${volumes.length})`}>
      <div className="flex flex-col px-3 py-2" style={{ fontSize: 'var(--text-micro)' }}>
        {volumes.map((volume) => (
          <div key={volume.name} className="flex items-baseline gap-2 py-0.5">
            <span className="min-w-0 flex-1 truncate font-mono" style={{ color: 'var(--text-primary)' }}>
              {volume.name}
            </span>
            <span className="shrink-0" style={{ color: 'var(--text-secondary)' }}>
              {volume.type}
            </span>
            {volume.source && (
              <span className="min-w-0 max-w-[45%] truncate font-mono" style={{ color: 'var(--text-muted)' }} title={volume.source}>
                {volume.source}
              </span>
            )}
          </div>
        ))}
      </div>
    </Group>
  )
}

/**
 * One labelled line inside a container block.
 *
 * The labels carry the accent and the values do not: a container block is a short list
 * of unlike things — an address, a port, two quantities, a path — and the label is what
 * the eye lands on to find the one it came for.
 */
function ContainerField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[4.5rem_1fr] items-baseline gap-2 py-0.5" style={{ fontSize: 'var(--text-micro)' }}>
      <dt className="uppercase tracking-[0.04em]" style={{ color: 'var(--accent)' }}>
        {label}
      </dt>
      <dd className="min-w-0" style={{ color: 'var(--text-secondary)' }}>
        {children}
      </dd>
    </div>
  )
}

/**
 * What a container is doing, in the words the rest of the interface uses.
 *
 * "not ready" was the only thing said about a container that was not ready, which is
 * the state of every init container that has done its job and of every container still
 * starting. The state itself says more, and says it the same way the status column does.
 */
function containerState(containerStatus: Record<string, unknown> | undefined): {
  label: string
  detail: string
} {
  if (!containerStatus) return { label: '', detail: '' }

  const state = (containerStatus.state ?? {}) as Record<string, unknown>
  const running = state.running as Record<string, unknown> | undefined
  const waiting = state.waiting as Record<string, unknown> | undefined
  const terminated = state.terminated as Record<string, unknown> | undefined

  if (waiting) return { label: text(waiting.reason) || 'Waiting', detail: text(waiting.message) }
  if (terminated) {
    const reason = text(terminated.reason) || 'Terminated'
    const code = terminated.exitCode
    return {
      label: reason,
      detail: typeof code === 'number' ? `exit code ${code}` : text(terminated.message),
    }
  }
  if (running) return { label: containerStatus.ready === true ? 'Running' : 'Starting', detail: '' }
  return { label: '', detail: '' }
}

/**
 * The registry an image resolves to, under the reference itself.
 *
 * The reference alone does not answer where a container comes from: a name with no host
 * in it is Docker Hub, which is worth saying out loud in an environment whose nodes are
 * only supposed to pull from a mirror.
 */
function ImageOrigin({
  image,
  pullPolicy,
  imageID,
}: {
  image: string
  pullPolicy: string
  imageID: string
}) {
  const origin = registryOf(image)
  const pulled = pulledFrom(image, imageID)

  return (
    <div className="flex flex-col" style={{ color: 'var(--text-muted)' }}>
      <div className="flex flex-wrap gap-x-3">
        {origin && (
          <span>
            registry{' '}
            <span className="font-mono" style={{ color: 'var(--text-secondary)' }}>
              {origin.host}
            </span>
            {origin.implicit && ' (implicit)'}
          </span>
        )}
        {pullPolicy && (
          <span>
            pull <span className="font-mono">{pullPolicy}</span>
          </span>
        )}
      </div>

      {pulled && (
        <div>
          pulled{' '}
          <span className="font-mono break-all" style={{ color: 'var(--text-secondary)' }} title={pulled}>
            {pulled}
          </span>
        </div>
      )}
    </div>
  )
}

/**
 * Usage against its request and limit.
 *
 * The bar is scaled to the limit where one exists, because that is the number that
 * causes a kill; without a limit it scales to the request, which is what scheduling
 * was based on.
 */
function UsageBar({
  label,
  used,
  request,
  limit,
  format,
}: {
  label: string
  used: number
  request: number
  limit: number
  format: (value: number) => string
}) {
  const ceiling = limit || request || Math.max(used, 1)
  const usedFraction = Math.min(1, used / ceiling)
  const requestFraction = request && ceiling ? Math.min(1, request / ceiling) : 0

  const nearLimit = limit > 0 && used / limit > 0.85
  const color = nearLimit ? 'var(--status-warn)' : 'var(--accent)'

  return (
    <div className="mb-1.5">
      <div className="mb-0.5 flex items-baseline justify-between" style={{ fontSize: 'var(--text-micro)' }}>
        <span style={{ color: 'var(--text-muted)' }}>{label}</span>
        <span className="font-mono" style={{ color: 'var(--text-secondary)' }}>
          {format(used)}
          {limit > 0 && <span style={{ color: 'var(--text-muted)' }}> / {format(limit)}</span>}
          {limit === 0 && request > 0 && (
            <span style={{ color: 'var(--text-muted)' }}> · req {format(request)}</span>
          )}
          {limit === 0 && request === 0 && <span style={{ color: 'var(--text-muted)' }}> · no limit</span>}
        </span>
      </div>

      <div
        className="relative h-1.5 w-full overflow-hidden"
        style={{ backgroundColor: 'var(--bg-raised)', borderRadius: '1px' }}
      >
        <div
          className="absolute inset-y-0 left-0 transition-[width] duration-300"
          style={{ width: `${usedFraction * 100}%`, backgroundColor: color }}
        />
        {requestFraction > 0 && (
          <div
            title={`request ${format(request)}`}
            className="absolute inset-y-0 w-px"
            style={{ left: `${requestFraction * 100}%`, backgroundColor: 'var(--text-muted)' }}
          />
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------- pieces

/** One of the measurements a node's chart can draw, as an icon beside the tabs. */
function MetricTab({
  label,
  active,
  onClick,
  path,
}: {
  label: string
  active: boolean
  onClick: () => void
  path: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-pressed={active}
      title={label}
      className="flex h-7 w-7 items-center justify-center transition-colors hover:bg-[var(--bg-hover)]"
      style={{
        borderRadius: 'var(--radius-sharp)',
        color: active ? 'var(--accent)' : 'var(--text-muted)',
        backgroundColor: active ? 'var(--accent-muted)' : undefined,
      }}
    >
      <svg width="18" height="18" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <path d={path} stroke="currentColor" strokeWidth="1.15" strokeLinecap="round" />
      </svg>
    </button>
  )
}

function Group({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-b" style={{ borderColor: 'var(--border-subtle)' }}>
      <h3
        className="px-3 pb-1 pt-3 font-semibold uppercase tracking-[0.08em]"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
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
      className="grid grid-cols-[7.5rem_1fr] items-baseline gap-2 px-3 py-1"
      style={{ fontSize: 'var(--text-micro)' }}
    >
      <dt style={{ color: 'var(--text-muted)' }}>{label}</dt>
      <dd className="truncate font-mono" style={{ color: 'var(--text-primary)' }}>
        {children}
      </dd>
    </div>
  )
}

function LinkValue({ value, onClick }: { value: string; onClick?: (() => void) | undefined }) {
  if (!onClick) return <span>{value}</span>

  return (
    <button type="button" onClick={onClick} className="truncate hover:underline" style={{ color: 'var(--status-info)' }}>
      {value}
    </button>
  )
}

/** Actions that exist, and the ones later phases will enable. */
/**
 * The object's actions, as icons, beside its name.
 *
 * The same registry the right-click menu reads (ADR-053), in the same order, so the two
 * cannot come to disagree about what an object can do. Actions a later phase brings are
 * left out here rather than shown greyed: a menu has room to explain itself, a row of
 * icons does not.
 */
function ActionIcons({ kind, onAction }: { kind: string; onAction: (id: string) => void }) {
  const actions = availableActionsFor(kind)
  if (actions.length === 0) return null

  return (
    <span className="flex shrink-0 items-center gap-0.5">
      {actions.map((action) => (
        <button
          key={action.id}
          type="button"
          onClick={() => onAction(action.id)}
          title={action.shortcut ? `${action.label} (${action.shortcut})` : action.label}
          aria-label={action.label}
          className="flex h-8 w-8 items-center justify-center transition-colors hover:bg-[var(--bg-hover)]"
          style={{
            borderRadius: 'var(--radius-sharp)',
            color: action.destructive ? 'var(--status-error)' : 'var(--text-secondary)',
          }}
        >
          <span className="scale-[1.35]">{action.icon}</span>
        </button>
      ))}

      <span className="mx-0.5 h-4 w-px" style={{ backgroundColor: 'var(--border-default)' }} />
    </span>
  )
}

function DrawerSkeleton() {
  return (
    <div className="flex flex-col gap-2 p-3" aria-label="Loading">
      {[0, 1, 2, 3, 4, 5, 6].map((row) => (
        <div
          key={row}
          className="animate-pulse"
          style={{
            height: 10,
            width: `${88 - row * 7}%`,
            borderRadius: 'var(--radius-sharp)',
            backgroundColor: 'var(--bg-raised)',
          }}
        />
      ))}
    </div>
  )
}

function PaneTab({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className="border-r px-3 transition-colors"
      style={{
        borderColor: 'var(--border-subtle)',
        fontSize: 'var(--text-micro)',
        backgroundColor: active ? 'var(--bg-base)' : 'transparent',
        color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
        boxShadow: active ? 'inset 0 -2px 0 0 var(--accent)' : undefined,
      }}
    >
      {label}
    </button>
  )
}

/**
 * Renders an unknown field as text.
 *
 * These objects come from the API server as untyped JSON, so a field that is expected
 * to be a string may not be. Anything that is not a primitive becomes an empty string
 * rather than "[object Object]".
 */
function text(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

/** Kubernetes quantities, reduced to plain numbers for the bars. */
function parseMilli(value: string): number {
  if (!value || value === '—') return 0
  if (value.endsWith('m')) return Number(value.slice(0, -1)) || 0
  return (Number(value) || 0) * 1000
}

function parseMi(value: string): number {
  if (!value || value === '—') return 0

  const units: [string, number][] = [
    ['Gi', 1024],
    ['Mi', 1],
    ['Ki', 1 / 1024],
    ['G', 1000 / 1024 / 1.048576],
    ['M', 1 / 1.048576],
  ]
  for (const [suffix, factor] of units) {
    if (value.endsWith(suffix)) return (Number(value.slice(0, -suffix.length)) || 0) * factor
  }
  return (Number(value) || 0) / (1024 * 1024)
}
