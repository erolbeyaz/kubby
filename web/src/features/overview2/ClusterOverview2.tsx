import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { Callout } from '@/components/Callout'
import { Icon } from '@/components/Icon'
import {
  ApiError,
  api,
  type Cluster,
  type ClusterHealthMetrics,
  type NamedSeries,
} from '@/lib/api'

import { environmentColor } from '@/features/clusters/environment'
import { FleetHealth } from '@/features/health/FleetHealth'
import { AreaChart, SERIES_COLOURS, format, formatBytes } from '@/features/metrics/charts'

import { CapacityPanels } from './Capacity'
import { Inventory } from './Inventory'
import { NodeConditions, Placement } from './Insights'
import { NodeHealthTable } from './NodeHealth'
import { BarRow, Figure, Grid, HealthRow, Kpi, Panel, Quiet, RADIUS, Section } from './parts'
import {
  AlertTable,
  DegradedTable,
  EventTable,
  PodProblemTable,
  StorageTable,
} from './tables'

const WINDOWS = ['15m', '1h', '6h', '24h'] as const
type Window = (typeof WINDOWS)[number]

/**
 * Overview 2 — an alternative to the cluster Overview, built to a supplied design.
 *
 * The layout is the brief's: a status banner, a five-across strip of headline numbers,
 * then capacity, nodes, trends, workloads, errors, control plane and storage. The colours
 * and the data are Kubby's.
 *
 * Two rules from the existing screens carry over and are not negotiable here either.
 * Every number that counts objects opens them — a count that cannot be clicked through is
 * a count somebody has to go and search for. And a metric nobody collects reads N/A, never
 * zero: on this screen zero is the most reassuring thing on the page.
 */
export function ClusterOverview2({
  cluster,
  onOpenObject,
  onOpenType,
  onOpenCluster,
}: {
  cluster: Cluster
  onOpenObject: (kind: string, namespace: string, name: string) => void
  onOpenType: (typeKey: string, namespace?: string) => void
  /** Switching clusters from the strip at the top. Omitted, the strip is left off. */
  onOpenCluster?: ((clusterId: string) => void) | undefined
}) {
  const [window, setWindow] = useState<Window>('6h')
  const [problemsOnly, setProblemsOnly] = useState(false)

  const metrics = useQuery({
    queryKey: ['cluster-metrics', cluster.id, window],
    queryFn: ({ signal }) => api.clusterMetrics(cluster.id, window, signal),
    refetchInterval: 30_000,
  })

  const events = useQuery({
    queryKey: ['workloads-overview', cluster.id, ''],
    queryFn: ({ signal }) => api.workloadsOverview(cluster.id, [], signal),
    refetchInterval: 30_000,
  })

  if (metrics.isError) {
    const error = metrics.error instanceof ApiError ? metrics.error : null
    return (
      <div className="p-4">
        <Callout tone="error" title="Could not read this cluster" requestId={error?.requestId}>
          {error?.message ?? 'The metrics request failed.'}
        </Callout>
      </div>
    )
  }

  if (metrics.data && !metrics.data.configured) {
    return (
      <div className="p-4">
        <Callout tone="info" title="No Prometheus found in this cluster">
          This screen is built entirely on Prometheus. Add its address under the cluster's
          settings, or install one in the cluster and Kubby will find it.
        </Callout>
      </div>
    )
  }

  const health = metrics.data?.health
  const summary = health?.summary
  const extras = health?.extras

  return (
    <div className="h-full overflow-auto px-4 pb-8 pt-4">
      {/* The rest of the fleet, above this cluster's own numbers — the same strip the
          Overview carries. Reading one cluster with the others out of view is how a
          fleet-wide outage reads as a single cluster's problem, and the card marked
          current is the second answer to "which cluster is this". */}
      {onOpenCluster && (
        <div className="mb-3 border-b pb-2" style={{ borderColor: 'var(--border-subtle)' }}>
          <FleetHealth currentId={cluster.id} onOpen={onOpenCluster} />
        </div>
      )}

      <Header
        cluster={cluster}
        endpoint={metrics.data?.endpoint}
        window={window}
        onWindow={setWindow}
        onRefresh={() => void metrics.refetch()}
        refreshing={metrics.isFetching}
      />

      {/* What the cluster is made of, directly under its name — the inventory belongs to
          the cluster's identity, so it sits with it rather than in a section further
          down. Light enough not to compete with the verdict beneath it. */}
      <Inventory counts={events.data?.counts ?? []} onOpenType={onOpenType} />

      <StatusBanner summary={summary} loading={metrics.isPending} />

      {/* The headline strip. Five across on a wide screen, folding to three, two and one —
          the brief's own breakpoints. */}
      <Grid columns={5}>
        <Kpi
          label="Node health"
          icon="node"
          value={
            <>
              <Figure reading={summary?.nodesReady ?? unknownReading} />
              <span style={{ color: 'var(--text-muted)' }}> / </span>
              <Figure reading={summary?.nodesTotal ?? unknownReading} />
            </>
          }
          detail={nodeDetail(summary)}
          tone={raised(summary?.nodesNotReady) ? 'var(--status-error)' : 'var(--status-ok)'}
          unknown={!summary?.nodesReady.known}
          onOpen={() => onOpenType('nodes')}
        />
        <Kpi
          label="Pod health"
          icon="pod"
          value={
            <>
              <Figure reading={summary?.podsReady ?? unknownReading} />
              <span style={{ color: 'var(--text-muted)' }}> / </span>
              <Figure reading={summary?.podsTotal ?? unknownReading} />
            </>
          }
          detail={podDetail(summary)}
          tone="var(--status-ok)"
          unknown={!summary?.podsReady.known}
          onOpen={() => onOpenType('pods')}
        />
        <Kpi
          label="Restarts"
          icon="restart"
          value={<Figure reading={summary?.restarts1h ?? unknownReading} />}
          detail={restartDetail(extras)}
          tone={
            (summary?.restarts1h.value ?? 0) > 10 ? 'var(--status-warn)' : 'var(--text-primary)'
          }
          unknown={!summary?.restarts1h.known}
        />
        <Kpi
          label="OOM / Eviction"
          icon="memory"
          value={
            <>
              <Figure reading={summary?.oomKilled ?? unknownReading} />
              <span style={{ color: 'var(--text-muted)' }}> / </span>
              <Figure reading={summary?.evicted ?? unknownReading} />
            </>
          }
          detail="killed by a limit · pushed off a node"
          tone={raised(summary?.oomKilled) ? 'var(--status-error)' : 'var(--text-primary)'}
          unknown={!summary?.oomKilled.known && !summary?.evicted.known}
        />
        <Kpi
          label="Workloads"
          icon="workload"
          value={
            <>
              {format(healthyWorkloads(health))}
              <span style={{ color: 'var(--text-muted)' }}> / {format(totalWorkloads(health))}</span>
            </>
          }
          detail={workloadDetail(summary, extras)}
          tone={raised(summary?.unavailableWorkloads) ? 'var(--status-warn)' : 'var(--status-ok)'}
          unknown={(health?.workloads?.length ?? 0) === 0}
          onOpen={() => onOpenType('apps/deployments')}
        />
        <Kpi
          label="Active alerts"
          icon="alert"
          value={
            <>
              <Figure reading={summary?.alertsCritical ?? unknownReading} />
              <span style={{ color: 'var(--text-muted)' }}> / </span>
              <Figure reading={summary?.alertsWarning ?? unknownReading} />
            </>
          }
          detail="critical / warning"
          tone={raised(summary?.alertsCritical) ? 'var(--status-error)' : 'var(--text-primary)'}
          unknown={!summary?.alertsCritical.known}
        />
        <Kpi
          label="API server 5xx"
          icon="pulse"
          value={<Figure reading={summary?.apiErrorRate ?? unknownReading} render={(v) => v.toFixed(2)} suffix="%" />}
          detail={apiDetail(health)}
          tone={(summary?.apiErrorRate.value ?? 0) > 1 ? 'var(--status-error)' : 'var(--text-primary)'}
          unknown={!summary?.apiErrorRate.known}
        />
        <Kpi
          label="Pending pods"
          icon="pending"
          value={<Figure reading={summary?.podsPending ?? unknownReading} />}
          detail={pendingDetail(summary)}
          tone={raised(summary?.podsPending) ? 'var(--status-warn)' : 'var(--text-primary)'}
          unknown={!summary?.podsPending.known}
          onOpen={() => onOpenType('pods')}
        />
        <Kpi
          label="Scrape targets"
          icon="scrape"
          value={
            <>
              {summary?.targetsTotal.known && summary.targetsDown.known
                ? format(summary.targetsTotal.value - summary.targetsDown.value)
                : '—'}
              <span style={{ color: 'var(--text-muted)' }}>
                {' / '}
                <Figure reading={summary?.targetsTotal ?? unknownReading} />
              </span>
            </>
          }
          detail={targetDetail(summary)}
          tone={raised(summary?.targetsDown) ? 'var(--status-error)' : 'var(--status-ok)'}
          unknown={!summary?.targetsTotal.known}
        />
        <Kpi
          label="Services without endpoints"
          icon="unplug"
          value={format(extras?.serviceGaps?.length ?? 0)}
          detail={extras?.serviceGaps?.[0] ? `${extras.serviceGaps[0].namespace}/${extras.serviceGaps[0].name}` : 'every Service is answered'}
          tone={(extras?.serviceGaps?.length ?? 0) > 0 ? 'var(--status-warn)' : 'var(--status-ok)'}
          unknown={extras ? !extras.servicesKnown : true}
        />
      </Grid>

      <Section title="Cluster capacity" hint="real usage and scheduler headroom together">
        <CapacityPanels health={health} />
      </Section>

      <Section
        title="Node health"
        hint="pressure, capacity, disk, network and agent reachability"
        right={
          <label
            className="flex cursor-pointer items-center gap-2"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
          >
            <input
              type="checkbox"
              checked={problemsOnly}
              onChange={(event) => setProblemsOnly(event.target.checked)}
            />
            Only nodes with problems
          </label>
        }
      >
        <div
          className="overflow-hidden border"
          style={{ borderRadius: RADIUS, borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
        >
          <NodeHealthTable
            nodes={health?.nodeDetails ?? []}
            problemsOnly={problemsOnly}
            onOpen={(name) => onOpenObject('Node', '', name)}
          />
        </div>
      </Section>

      <Section title="Resource trends" hint={`last ${window} · cluster wide`}>
        <Grid columns={2}>
          <Panel title="CPU usage" meta={cpuMeta(health)}>
            <AreaChart series={lines(health?.trends?.cpuByNodeOverTime)} unit="%" />
          </Panel>
          <Panel title="Memory usage" meta={memoryMeta(health)}>
            <AreaChart series={lines(health?.trends?.memoryByNodeOverTime)} unit="%" />
          </Panel>
          <Panel title="Disk I/O wait" meta="per node">
            <AreaChart series={lines(health?.trends?.ioWaitByNode)} unit="%" />
          </Panel>
          <Panel title="Network throughput" meta="cluster wide, per second">
            <AreaChart
              series={throughput(health?.trends)}
              render={(value) => `${formatBytes(value)}/s`}
            />
          </Panel>
        </Grid>
      </Section>

      <Section title="Pod and workload health">
        <Grid columns={3}>
          <Panel title="Pod status" meta={`${format(totalPods(health))} pods`}>
            <div className="flex flex-col">
              <HealthRow
                name="Running and ready"
                detail="passing readiness, taking traffic"
                value={
                  extras?.containersReady.known
                    ? `${format(health?.pods.running ?? 0)}`
                    : format(health?.pods.running ?? 0)
                }
                tone="var(--status-ok)"
                onOpen={() => onOpenType('pods')}
              />
              <HealthRow
                name="Pending"
                detail={pendingDetail(summary)}
                value={format(health?.pods.pending ?? 0)}
                tone={(health?.pods.pending ?? 0) > 0 ? 'var(--status-warn)' : undefined}
                onOpen={() => onOpenType('pods')}
              />
              {/* Its own row rather than a share of Pending. These pods were placed and
                  then stopped by their own containers, and the fix is an image or a
                  config, not room in the cluster. */}
              <HealthRow
                name="Will not start"
                detail="placed, then held back by a container"
                value={format(health?.pods.notStarting ?? 0)}
                tone={(health?.pods.notStarting ?? 0) > 0 ? 'var(--status-error)' : undefined}
                onOpen={() => onOpenType('pods')}
              />
              <HealthRow
                name="Failed or unknown"
                detail="gave up, or the node stopped answering for them"
                value={format((health?.pods.failed ?? 0) + (health?.pods.unknown ?? 0))}
                tone={(health?.pods.failed ?? 0) > 0 ? 'var(--status-error)' : undefined}
                onOpen={() => onOpenType('pods')}
              />
              <HealthRow
                name="Containers not ready"
                detail="running is not the same as working"
                value={
                  extras?.containersReady.known && extras.containersTotal.known
                    ? format(extras.containersTotal.value - extras.containersReady.value)
                    : <span style={{ color: 'var(--text-muted)' }}>N/A</span>
                }
                tone={notReadyContainers(extras) > 0 ? 'var(--status-warn)' : undefined}
              />
            </div>
          </Panel>

          <div className="md:col-span-2" style={{ gridColumn: 'span 2' }}>
            <Panel
              title="Problem pods"
              meta={`${podProblems(health).length} of ${format(totalPods(health))} · most critical first`}
              tone={podProblems(health).some((p) => p.severity === 'error') ? 'error' : undefined}
            >
              <PodProblemTable
                rows={podProblems(health)}
                onOpen={(namespace, pod) => onOpenObject('Pod', namespace, pod)}
              />
            </Panel>
          </div>
        </Grid>
      </Section>

      <Section title="Workload and resource behaviour">
        <Panel
          title="Degraded workloads"
          meta={`${degraded(health).length} short of desired`}
          tone={degraded(health).length > 0 ? 'warn' : undefined}
        >
          <DegradedTable
            rows={degraded(health)}
            scalers={extras?.scalers ?? []}
            onOpen={onOpenObject}
          />
        </Panel>

        <Grid columns={4}>
          <Panel title="Resource policy" meta="containers with nothing set">
            <div className="flex flex-col">
              <HealthRow
                name="No CPU request"
                detail="the scheduler is guessing where these fit"
                value={format(riskCount(health, 'no-cpu-requests'))}
                tone={riskCount(health, 'no-cpu-requests') > 0 ? 'var(--status-warn)' : undefined}
              />
              <HealthRow
                name="No memory request"
                detail="the same, for the resource that kills pods"
                value={format(riskCount(health, 'no-requests'))}
                tone={riskCount(health, 'no-requests') > 0 ? 'var(--status-warn)' : undefined}
              />
              <HealthRow
                name="No memory limit"
                detail="one pod can take a node with it"
                value={format(riskCount(health, 'no-limits'))}
                tone={riskCount(health, 'no-limits') > 0 ? 'var(--status-warn)' : undefined}
              />
              <HealthRow
                name="Autoscalers at ceiling"
                detail="cannot answer more load"
                value={
                  extras?.scalersKnown ? (
                    format((extras.scalers ?? []).filter((s) => s.atCeiling).length)
                  ) : (
                    <span style={{ color: 'var(--text-muted)' }}>N/A</span>
                  )
                }
              />
            </div>
          </Panel>

          <Panel title="Top CPU" meta="cores in use">
            {(health?.topCpu?.length ?? 0) === 0 ? (
              <Quiet>No container usage is being reported.</Quiet>
            ) : (
              <div className="flex flex-col gap-1.5 pt-1">
                {(health?.topCpu ?? []).slice(0, 6).map((row) => (
                  <BarRow
                    key={row.name}
                    name={row.name}
                    percent={share(row.value, health?.topCpu)}
                    value={cores(row.value)}
                    tone={SERIES_COLOURS[0]}
                  />
                ))}
              </div>
            )}
          </Panel>

          <Panel title="Top memory" meta="working set">
            {(health?.topMemory?.length ?? 0) === 0 ? (
              <Quiet>No container usage is being reported.</Quiet>
            ) : (
              <div className="flex flex-col gap-1.5 pt-1">
                {(health?.topMemory ?? []).slice(0, 6).map((row) => (
                  <BarRow
                    key={row.name}
                    name={row.name}
                    percent={share(row.value, health?.topMemory)}
                    value={formatBytes(row.value)}
                    tone={SERIES_COLOURS[1]}
                  />
                ))}
              </div>
            )}
          </Panel>

          <Panel title="CPU throttling" meta="held back by their own limit">
            {throttled(health).length === 0 ? (
              <Quiet>No container is being throttled.</Quiet>
            ) : (
              <div className="flex flex-col gap-1.5 pt-1">
                {throttled(health)
                  .slice(0, 6)
                  .map((risk) => (
                    <BarRow
                      key={`${risk.namespace}/${risk.pod}/${risk.container}`}
                      name={`${risk.namespace}/${risk.pod}`}
                      percent={risk.value ?? 0}
                      value={`${format(risk.value ?? 0)}%`}
                      tone="var(--status-warn)"
                      onOpen={() => onOpenObject('Pod', risk.namespace, risk.pod)}
                    />
                  ))}
              </div>
            )}
          </Panel>
        </Grid>
      </Section>

      <Section title="Errors and events">
        <Grid columns={2}>
          <Panel
            title="Active alerts"
            meta={summary?.alertsCritical.known ? `${health?.alerts?.length ?? 0} firing` : 'N/A'}
          >
            <AlertTable
              rows={health?.alerts ?? []}
              known={summary?.alertsCritical.known ?? false}
              onOpen={onOpenObject}
            />
          </Panel>

          <Grid columns={2}>
            <Panel title="Termination reasons" meta="why containers stopped">
              {(health?.reasons?.length ?? 0) === 0 ? (
                <Quiet>Nothing has terminated abnormally.</Quiet>
              ) : (
                <div className="flex flex-col gap-1.5 pt-1">
                  {(health?.reasons ?? []).map((row) => (
                    <BarRow
                      key={row.name}
                      name={row.name}
                      percent={share(row.value, health?.reasons)}
                      value={format(row.value)}
                      tone="var(--status-error)"
                    />
                  ))}
                </div>
              )}
            </Panel>

            <Panel title="Waiting reasons" meta="why they will not start">
              {(health?.waiting?.length ?? 0) === 0 ? (
                <Quiet>Nothing is stuck waiting.</Quiet>
              ) : (
                <div className="flex flex-col gap-1.5 pt-1">
                  {(health?.waiting ?? []).map((row) => (
                    <BarRow
                      key={row.name}
                      name={row.name}
                      percent={share(row.value, health?.waiting)}
                      value={format(row.value)}
                      tone="var(--status-warn)"
                    />
                  ))}
                </div>
              )}
            </Panel>
          </Grid>
        </Grid>

        <Panel
          title="Recent Kubernetes warning events"
          meta="what the cluster is complaining about"
        >
          <EventTable rows={events.data?.events?.rows ?? []} failed={events.isError} />
        </Panel>
      </Section>

      <Section title="Control plane and platform services" hint="N/A means the endpoint is not scraped">
        <Grid columns={4}>
          <Panel title="API server" meta={<Figure reading={health?.controlPlane?.apiServers ?? unknownReading} suffix=" up" />}>
            <div className="flex flex-col">
              <HealthRow name="Latency p99" value={<Figure reading={health?.controlPlane?.apiLatencyP99 ?? unknownReading} render={seconds} />} />
              <HealthRow name="Latency p95" value={<Figure reading={health?.controlPlane?.apiLatencyP95 ?? unknownReading} render={seconds} />} />
              <HealthRow name="4xx" value={<Figure reading={health?.controlPlane?.apiErrors4xx ?? unknownReading} render={(v) => v.toFixed(2)} suffix="%" />} />
              <HealthRow name="5xx" value={<Figure reading={health?.controlPlane?.apiErrors5xx ?? unknownReading} render={(v) => v.toFixed(2)} suffix="%" />} />
            </div>
          </Panel>

          <Panel title="etcd" meta={<Figure reading={health?.controlPlane?.etcdMembers ?? unknownReading} suffix=" members" />}>
            <div className="flex flex-col">
              <HealthRow
                name="Leader"
                value={
                  health?.controlPlane?.etcdHasLeader.known
                    ? health.controlPlane.etcdHasLeader.value > 0
                      ? 'present'
                      : 'lost'
                    : 'N/A'
                }
                tone={
                  health?.controlPlane?.etcdHasLeader.known && health.controlPlane.etcdHasLeader.value === 0
                    ? 'var(--status-error)'
                    : undefined
                }
              />
              <HealthRow name="Leader changes · 1h" value={<Figure reading={health?.controlPlane?.etcdLeaderChanges ?? unknownReading} />} />
              <HealthRow name="Database" value={<Figure reading={health?.controlPlane?.etcdDbBytes ?? unknownReading} render={formatBytes} />} />
              <HealthRow name="fsync p99" value={<Figure reading={health?.controlPlane?.etcdFsyncP99 ?? unknownReading} render={seconds} />} />
            </div>
          </Panel>

          <Panel title="Scheduler and DNS">
            <div className="flex flex-col">
              <HealthRow name="Scheduling attempts/s" value={<Figure reading={health?.controlPlane?.schedulerAttempts ?? unknownReading} render={(v) => v.toFixed(2)} />} />
              <HealthRow name="Unschedulable" value={<Figure reading={health?.controlPlane?.schedulerUnschedulable ?? unknownReading} />} />
              <HealthRow name="CoreDNS instances" value={<Figure reading={health?.controlPlane?.corednsUp ?? unknownReading} />} />
              <HealthRow name="CoreDNS errors" value={<Figure reading={health?.controlPlane?.corednsErrorRate ?? unknownReading} render={(v) => v.toFixed(2)} suffix="%" />} />
            </div>
          </Panel>

          <Panel title="Controllers and monitoring">
            <div className="flex flex-col">
              <HealthRow name="Work queue depth" value={<Figure reading={health?.controlPlane?.controllerQueueDepth ?? unknownReading} />} />
              <HealthRow name="Retries/s" value={<Figure reading={health?.controlPlane?.controllerRetries ?? unknownReading} render={(v) => v.toFixed(1)} />} />
              <HealthRow name="Rule failures · 1h" value={<Figure reading={health?.controlPlane?.ruleFailures ?? unknownReading} />} />
              <HealthRow
                name="Targets down"
                value={<Figure reading={health?.controlPlane?.scrapeFailures ?? unknownReading} />}
                tone={raised(health?.controlPlane?.scrapeFailures) ? 'var(--status-error)' : undefined}
              />
            </div>
          </Panel>
        </Grid>
      </Section>

      <Section title="Storage, quota and reachability">
        <Panel
          title="Storage problems"
          meta={`${health?.storageProblems?.length ?? 0} claim(s) not bound`}
          tone={(health?.storageProblems?.length ?? 0) > 0 ? 'warn' : undefined}
        >
          <StorageTable
            rows={health?.storageProblems ?? []}
            onOpen={(namespace, name) => onOpenObject('PersistentVolumeClaim', namespace, name)}
          />
        </Panel>

        <Grid columns={3}>
          <Panel title="Namespace quota" meta="used against the hard limit">
            {!health?.controlPlane?.quotaNearLimit.known ? (
              <Quiet>
                No ResourceQuota is defined in this cluster, so there is nothing to be near
                — N/A rather than zero.
              </Quiet>
            ) : (
              <div className="flex flex-col">
                <HealthRow
                  name="Quotas near their limit"
                  detail="above 90% of the hard limit"
                  value={<Figure reading={health.controlPlane.quotaNearLimit} />}
                  tone={
                    health.controlPlane.quotaNearLimit.value > 0 ? 'var(--status-warn)' : undefined
                  }
                />
              </div>
            )}
          </Panel>

          <Panel title="Certificate expiry" meta="the one that goes first">
            <div className="flex flex-col">
              <HealthRow
                name="Soonest expiry"
                detail="across the certificates Prometheus can see"
                value={<Figure reading={health?.controlPlane?.certExpiryDays ?? unknownReading} suffix=" d" />}
                tone={
                  (health?.controlPlane?.certExpiryDays.value ?? 999) < 14
                    ? 'var(--status-error)'
                    : undefined
                }
              />
              <HealthRow
                name="Volumes provisioned"
                detail={
                  health?.controlPlane?.volumesBound.known
                    ? `${format(health.controlPlane.volumesBound.value)} bound`
                    : undefined
                }
                value={<Figure reading={health?.controlPlane?.volumeCapacityBytes ?? unknownReading} render={formatBytes} />}
              />
              <HealthRow
                name="Requested by claims"
                value={<Figure reading={health?.controlPlane?.volumeRequestedBytes ?? unknownReading} render={formatBytes} />}
              />
            </div>
          </Panel>

          <Panel
            title="Service reachability"
            meta="nothing else reports these"
            tone={(extras?.serviceGaps?.length ?? 0) > 0 ? 'warn' : undefined}
          >
            {!extras?.servicesKnown ? (
              <Quiet>No endpoint metrics are being collected — N/A rather than none.</Quiet>
            ) : (extras.serviceGaps?.length ?? 0) === 0 &&
              (extras.stalledRollouts?.length ?? 0) === 0 ? (
              <Quiet>Every Service has endpoints and no rollout is stalled.</Quiet>
            ) : (
              <div className="flex flex-col">
                {(extras.serviceGaps ?? []).slice(0, 4).map((gap) => (
                  <HealthRow
                    key={`${gap.namespace}/${gap.name}`}
                    name={`${gap.namespace}/${gap.name}`}
                    detail="no endpoints — nothing answers on it"
                    value="0 endpoints"
                    tone="var(--status-warn)"
                    onOpen={() => onOpenObject('Service', gap.namespace, gap.name)}
                  />
                ))}
                {(extras.stalledRollouts ?? []).slice(0, 3).map((rollout) => (
                  <HealthRow
                    key={`${rollout.namespace}/${rollout.name}`}
                    name={`${rollout.namespace}/${rollout.name}`}
                    detail="rollout not progressing"
                    value="Stalled"
                    tone="var(--status-warn)"
                    onOpen={() => onOpenObject(rollout.kind, rollout.namespace, rollout.name)}
                  />
                ))}
                <HealthRow
                  name="Ingress 5xx"
                  detail="requests the gateway could not answer"
                  value={<Figure reading={health?.controlPlane?.ingressErrorRate ?? unknownReading} render={(v) => v.toFixed(2)} suffix="%" />}
                />
              </div>
            )}
          </Panel>
        </Grid>
      </Section>

      {/* Two views the brief did not ask for, added because the data was already here and
          both answer questions the panels above raise but cannot settle: *when* did a node
          go wrong, and *what would be lost* if one went down. */}
      <Section title="Node conditions" hint="what the kubelet is complaining about, and for how long">
        <Panel
          title="Raised conditions"
          meta={`${health?.nodeDetails?.length ?? 0} nodes`}
          tone={conditionTone(health)}
        >
          <NodeConditions issues={health?.nodeIssues ?? []} nodes={health?.nodeDetails ?? []} />
        </Panel>
      </Section>

      <Section title="Placement" hint="what would be lost if one node went down">
        <Panel title="Pods per namespace" meta="most concentrated first">
          <Placement spread={health?.spread ?? []} nodes={spreadColumns(health?.spread)} />
        </Panel>
      </Section>

      {(health?.warnings?.length ?? 0) > 0 && (
        <p className="mt-4" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          Some readings are missing: {health?.warnings?.join(' · ')}
        </p>
      )}
    </div>
  )
}

function Header({
  cluster,
  endpoint,
  window,
  onWindow,
  onRefresh,
  refreshing,
}: {
  cluster: Cluster
  endpoint?: string | undefined
  window: Window
  onWindow: (next: Window) => void
  onRefresh: () => void
  refreshing: boolean
}) {
  const accent = environmentColor(cluster.environment, cluster.color)

  return (
    <header className="mb-3 flex flex-wrap items-center justify-between gap-3">
      {/* Which cluster, first and largest. Every number under this header belongs to one
          cluster, and reading them against the wrong one is the expensive mistake here —
          so the name leads, in the cluster's own environment colour, and "Cluster
          overview" drops to the line that carries the rest of the identity. */}
      <span className="flex min-w-0 items-center gap-3">
        <span
          className="flex h-10 w-10 shrink-0 items-center justify-center"
          style={{ borderRadius: RADIUS, backgroundColor: accent, color: 'var(--bg-base, #0b0e11)' }}
        >
          <Icon name="stack" />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="flex min-w-0 flex-wrap items-center gap-2">
            <span
              className="truncate"
              style={{ fontSize: 'var(--text-title)', fontWeight: 600, color: 'var(--text-primary)' }}
            >
              {cluster.name}
            </span>
            <span
              className="px-1.5 py-0.5 font-mono uppercase"
              style={{
                fontSize: 'var(--text-micro)',
                borderRadius: 'var(--radius-sharp)',
                border: `1px solid ${accent}`,
                color: accent,
              }}
            >
              {cluster.displayEnvironment || cluster.environment}
            </span>
          </span>
          <span
            className="truncate font-mono"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            title={cluster.apiServerUrl}
          >
            Cluster overview
            {cluster.k8sVersion ? ` · ${cluster.k8sVersion}` : ''}
            {cluster.apiServerUrl ? ` · ${cluster.apiServerUrl}` : ''}
            {endpoint ? ` · prometheus ${endpoint}` : ''}
          </span>
        </span>
      </span>

      <span className="flex items-center gap-1.5">
        {WINDOWS.map((option) => (
          <button
            key={option}
            type="button"
            onClick={() => onWindow(option)}
            className="tool-chip font-mono"
            style={{
              borderColor: option === window ? 'var(--accent)' : 'var(--border-default)',
              color: option === window ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            {option}
          </button>
        ))}
        <button
          type="button"
          onClick={onRefresh}
          className="tool-chip flex items-center gap-1.5"
          style={{ borderColor: 'var(--border-default)', color: 'var(--text-secondary)' }}
        >
          <Icon name="refresh" className={refreshing ? 'animate-spin' : undefined} />
          Refresh
        </button>
      </span>
    </header>
  )
}

/**
 * The verdict, and the conditions that produced it.
 *
 * The reasons are the point. "Degraded" on its own sends somebody hunting; "Degraded —
 * two workloads short of replicas, a pod pending for two days" is something to act on.
 */
function StatusBanner({
  summary,
  loading,
}: {
  summary: ClusterHealthMetrics['summary']
  loading: boolean
}) {
  const status = summary?.status ?? (loading ? 'Unknown' : 'Unknown')
  const tone =
    status === 'Healthy'
      ? 'var(--status-ok)'
      : status === 'Degraded'
        ? 'var(--status-warn)'
        : status === 'Critical'
          ? 'var(--status-error)'
          : 'var(--text-muted)'

  const reasons = summary?.reasons ?? []

  return (
    <div
      className="mb-[18px] flex items-start gap-3 border p-3"
      style={{
        borderRadius: RADIUS,
        borderColor: tone,
        backgroundColor: `color-mix(in srgb, ${tone} 10%, var(--bg-surface))`,
      }}
    >
      <span style={{ color: tone }} className="mt-0.5 shrink-0">
        <Icon name={status === 'Healthy' ? 'pulse' : 'alertTriangle'} />
      </span>
      <span className="min-w-0 flex-1">
        <span style={{ fontSize: 'var(--text-body)', fontWeight: 600, color: tone }}>
          Cluster status: {loading ? 'reading…' : status}
        </span>
        <span
          className="mt-1 block"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
        >
          {reasons.length > 0
            ? reasons.join(' · ')
            : status === 'Healthy'
              ? 'No node, workload or scrape problem found.'
              : loading
                ? 'Collecting.'
                : 'No reason was reported.'}
        </span>
      </span>
    </div>
  )
}

// ---------------------------------------------------------------- derivations

const unknownReading = { value: 0, known: false }

const raised = (reading?: { value: number; known: boolean }) =>
  reading?.known === true && reading.value > 0

const seconds = (v: number) => (v < 1 ? `${(v * 1000).toFixed(0)} ms` : `${v.toFixed(2)} s`)

/** Named series into chart lines, cycling the palette by position. */
/**
 * Network as two lines, not two per node.
 *
 * It used to draw every node's receive line and every node's transmit line on one chart,
 * both sets labelled with the node's name — so a three-node cluster produced six lines
 * and a legend that named each machine twice with no way to tell which row was which
 * direction. It was also the only panel in the row with twice as many legend rows as the
 * others, which is what made it the tall one.
 *
 * Summed per timestamp rather than per position: two nodes whose series start at
 * different times would otherwise be added together out of step.
 */
function throughput(trends: ClusterHealthMetrics['trends']) {
  const total = (series: NamedSeries[] | null | undefined) => {
    const byTime = new Map<string, number>()
    for (const line of series ?? []) {
      for (const point of line.points ?? []) {
        byTime.set(point.at, (byTime.get(point.at) ?? 0) + point.value)
      }
    }
    return [...byTime.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([at, value]) => ({ at, value }))
  }

  return [
    { name: 'received', points: total(trends?.networkRx), colour: SERIES_COLOURS[0] ?? '' },
    { name: 'transmitted', points: total(trends?.networkTx), colour: SERIES_COLOURS[1] ?? '' },
  ]
}

function lines(series: NamedSeries[] | null | undefined, offset = 0) {
  return (series ?? []).map((line, index) => ({
    name: line.name,
    points: line.points ?? [],
    colour: SERIES_COLOURS[(index + offset) % SERIES_COLOURS.length] ?? '',
  }))
}

function nodeDetail(summary: ClusterHealthMetrics['summary']): string {
  if (!summary) return ''
  const parts: string[] = []
  if (raised(summary.nodesNotReady)) parts.push(`${format(summary.nodesNotReady.value)} not ready`)
  if (raised(summary.nodesUnderPressure)) parts.push(`${format(summary.nodesUnderPressure.value)} under pressure`)
  if (raised(summary.nodesUnschedulable)) parts.push(`${format(summary.nodesUnschedulable.value)} cordoned`)
  return parts.length > 0 ? parts.join(' · ') : 'all reporting Ready'
}

function podDetail(summary: ClusterHealthMetrics['summary']): string {
  if (!summary) return ''
  const pending = summary.podsPending.known ? `${format(summary.podsPending.value)} pending` : ''
  // Named separately from pending. A pod whose image will not pull is in phase Pending
  // too, and folding it in put a number here that no row on the screen accounted for.
  const stuck =
    summary.containersNotStarting?.known && summary.containersNotStarting.value > 0
      ? `${format(summary.containersNotStarting.value)} will not start`
      : ''
  return [pending, stuck, 'ready means passing readiness'].filter(Boolean).join(' · ')
}

function restartDetail(extras: ClusterHealthMetrics['extras']): string {
  if (!extras) return 'in the last hour'
  const recent = extras.restarts15m.known ? `${format(extras.restarts15m.value)} in 15m` : ''
  const day = extras.restarts24h.known ? `${format(extras.restarts24h.value)} in 24h` : ''
  return [recent, day].filter(Boolean).join(' · ') || 'in the last hour'
}

function workloadDetail(
  summary: ClusterHealthMetrics['summary'],
  extras: ClusterHealthMetrics['extras'],
): string {
  const short = raised(summary?.unavailableWorkloads)
    ? `${format(summary?.unavailableWorkloads.value ?? 0)} short of replicas`
    : 'all at desired replicas'
  const stalled = (extras?.stalledRollouts?.length ?? 0) > 0
    ? `${extras?.stalledRollouts?.length} rollout stalled`
    : ''
  return [short, stalled].filter(Boolean).join(' · ')
}

function apiDetail(health: ClusterHealthMetrics | undefined): string {
  const p99 = health?.controlPlane?.apiLatencyP99
  return p99?.known ? `p99 ${seconds(p99.value)}` : 'latency N/A'
}

function pendingDetail(summary: ClusterHealthMetrics['summary']): string {
  const longest = summary?.longestPendingSeconds
  if (!longest?.known || longest.value <= 0) return 'waiting to start'
  const value = longest.value
  const text = value >= 86400 ? `${Math.floor(value / 86400)}d` : value >= 3600 ? `${Math.floor(value / 3600)}h` : `${Math.floor(value / 60)}m`
  return `longest ${text}`
}

function targetDetail(summary: ClusterHealthMetrics['summary']): string {
  return raised(summary?.targetsDown)
    ? `${format(summary?.targetsDown.value ?? 0)} down — panels may be stale`
    : 'every target answering'
}

const totalPods = (health: ClusterHealthMetrics | undefined) => {
  const pods = health?.pods
  if (!pods) return 0
  return (
    pods.running + pods.pending + pods.notStarting + pods.failed + pods.succeeded + pods.unknown
  )
}


const podProblems = (health: ClusterHealthMetrics | undefined) => health?.podProblems ?? []

const throttled = (health: ClusterHealthMetrics | undefined) =>
  (health?.extras?.risks ?? []).filter((risk) => risk.kind === 'throttled')

const notReadyContainers = (extras: ClusterHealthMetrics['extras']) =>
  extras?.containersReady.known && extras.containersTotal.known
    ? extras.containersTotal.value - extras.containersReady.value
    : 0

const cores = (value: number) =>
  value < 1 ? `${Math.round(value * 1000)}m` : value.toFixed(2).replace(/\.00$/, '')

const degraded = (health: ClusterHealthMetrics | undefined) =>
  (health?.workloads ?? []).filter((row) => row.desired > 0 && row.ready < row.desired)

const healthyWorkloads = (health: ClusterHealthMetrics | undefined) =>
  (health?.workloads ?? []).filter((row) => row.desired > 0 && row.ready >= row.desired).length

const totalWorkloads = (health: ClusterHealthMetrics | undefined) => health?.workloads?.length ?? 0

const riskCount = (health: ClusterHealthMetrics | undefined, kind: string) =>
  (health?.extras?.risks ?? []).filter((risk) => risk.kind === kind).length

function share(value: number, rows: Array<{ value: number }> | null | undefined): number {
  const peak = Math.max(...(rows ?? []).map((row) => row.value), 0.0001)
  return (value / peak) * 100
}

function cpuMeta(health: ClusterHealthMetrics | undefined): string {
  const capacity = health?.capacity
  if (!capacity) return ''
  return `${format(capacity.cpuCommittedPercent)}% requested of ${format(capacity.cores)} cores`
}

function memoryMeta(health: ClusterHealthMetrics | undefined): string {
  const capacity = health?.capacity
  if (!capacity) return ''
  return `${format(capacity.memoryCommittedPercent)}% requested of ${formatBytes(capacity.memoryBytes)}`
}



/** Red when a condition is raised, amber when a node is only cordoned or unscraped. */
function conditionTone(health: ClusterHealthMetrics | undefined): 'error' | 'warn' | undefined {
  const nodes = health?.nodeDetails ?? []
  if ((health?.nodeIssues?.length ?? 0) > 0 || nodes.some((node) => !node.ready)) return 'error'
  if (nodes.some((node) => node.unschedulable || !node.nodeExporterUp || !node.kubeletUp)) {
    return 'warn'
  }
  return undefined
}

function spreadColumns(spread: ClusterHealthMetrics['spread']): string[] {
  return [...new Set((spread ?? []).map((entry) => entry.node))].sort()
}
