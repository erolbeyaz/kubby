import type { ClusterHealthMetrics } from '@/lib/api'

import { format, formatBytes } from '@/features/metrics/charts'

import { Grid, Panel } from './parts'

/**
 * Capacity, as four figures rather than one percentage.
 *
 * The brief asks for real usage and scheduler headroom side by side, and it is right to:
 * a cluster at 31 cores used out of 60 allocatable *looks* half empty, but if 46 of those
 * cores are already requested only 14 can actually be scheduled. Those are the two
 * different answers to "have we got room", and showing one of them hides the other.
 *
 * The segmented bar reads left to right: what is running, what is promised but idle, and
 * what is genuinely free.
 */
export function CapacityPanels({ health }: { health: ClusterHealthMetrics | undefined }) {
  const capacity = health?.capacity
  const control = health?.controlPlane

  if (!capacity || capacity.nodes === 0) {
    return (
      <Panel title="Capacity">
        <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          No node metrics available.
        </p>
      </Panel>
    )
  }

  // Allocatable is what pods may be given; capacity is what the machines physically have.
  // The difference is what the kubelet and the OS reserved, and it is never schedulable.
  const cpuAllocatable = sum(health?.nodeDetails, (node) => node.cpuAllocatable) || capacity.cores
  const cpuUsed = weighted(health?.nodeDetails, (node) => node.cpuPercent, (node) => node.cores)
  const cpuRequested = (capacity.cpuCommittedPercent / 100) * cpuAllocatable

  const memAllocatable =
    sum(health?.nodeDetails, (node) => node.memoryAllocatable) || capacity.memoryBytes
  const memUsed = weighted(
    health?.nodeDetails,
    (node) => node.memoryPercent,
    (node) => node.memoryTotalBytes,
  )
  const memRequested = (capacity.memoryCommittedPercent / 100) * memAllocatable

  return (
    <Grid columns={2}>
      <Resource
        title="CPU"
        meta={`${format(capacity.cores)} core capacity`}
        headline={`${cpuUsed.toFixed(1)}`}
        headlineUnit="cores used"
        line={`${format(cpuAllocatable)} allocatable · ${cpuRequested.toFixed(1)} requested · ${schedulable(cpuAllocatable, cpuRequested).toFixed(1)} schedulable`}
        used={cpuUsed}
        requested={cpuRequested}
        total={cpuAllocatable}
        unit={(v) => `${v.toFixed(1)} cores`}
      />

      <Resource
        title="Memory"
        meta={`${formatBytes(capacity.memoryBytes)} capacity`}
        headline={formatBytes(memUsed)}
        headlineUnit="used"
        line={`${formatBytes(memAllocatable)} allocatable · ${formatBytes(memRequested)} requested · ${formatBytes(schedulable(memAllocatable, memRequested))} schedulable`}
        used={memUsed}
        requested={memRequested}
        total={memAllocatable}
        unit={formatBytes}
      />

      <Resource
        title="Pod slots"
        meta={`${format(capacity.podCapacity)} allocatable`}
        headline={format(capacity.pods)}
        headlineUnit="scheduled"
        line={`${format(capacity.podCapacity - capacity.pods)} slots free · ${format(
          capacity.podCapacity > 0 ? (capacity.pods / capacity.podCapacity) * 100 : 0,
        )}% full`}
        used={capacity.pods}
        requested={capacity.pods}
        total={capacity.podCapacity}
        unit={(v) => `${format(v)} pods`}
      />

      <Panel
        title="Persistent storage"
        meta={
          control?.volumeCapacityBytes.known
            ? `${formatBytes(control.volumeCapacityBytes.value)} provisioned`
            : 'N/A'
        }
      >
        {!control?.volumeCapacityBytes.known ? (
          <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            No persistent volume metrics are being collected.
          </p>
        ) : (
          <>
            <p className="pt-1">
              <span
                className="font-mono tabular-nums"
                style={{ fontSize: '21px', fontWeight: 500, letterSpacing: '-0.02em', color: 'var(--text-primary)' }}
              >
                {control.volumeRequestedBytes.known
                  ? formatBytes(control.volumeRequestedBytes.value)
                  : '—'}
              </span>
              <span className="ml-1.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                requested by claims
              </span>
            </p>
            <p className="mt-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
              {control.volumesBound.known ? `${format(control.volumesBound.value)} volumes bound` : ''}
              {control.volumeUsedBytes.known
                ? ` · ${formatBytes(control.volumeUsedBytes.value)} in use`
                : ' · usage needs kubelet volume stats'}
            </p>
            <Bar
              used={control.volumeRequestedBytes.known ? control.volumeRequestedBytes.value : 0}
              requested={control.volumeRequestedBytes.known ? control.volumeRequestedBytes.value : 0}
              total={control.volumeCapacityBytes.value}
            />
            <Legend
              used={control.volumeRequestedBytes.known ? control.volumeRequestedBytes.value : 0}
              requested={control.volumeRequestedBytes.known ? control.volumeRequestedBytes.value : 0}
              total={control.volumeCapacityBytes.value}
              unit={formatBytes}
              usedLabel="Requested"
            />
          </>
        )}
      </Panel>
    </Grid>
  )
}

function Resource({
  title,
  meta,
  headline,
  headlineUnit,
  line,
  used,
  requested,
  total,
  unit,
}: {
  title: string
  meta: string
  headline: string
  headlineUnit: string
  line: string
  used: number
  requested: number
  total: number
  unit: (value: number) => string
}) {
  return (
    <Panel title={title} meta={meta}>
      <p className="pt-1">
        <span
          className="font-mono tabular-nums"
          style={{ fontSize: '21px', fontWeight: 500, letterSpacing: '-0.02em', color: 'var(--text-primary)' }}
        >
          {headline}
        </span>
        <span className="ml-1.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {headlineUnit}
        </span>
      </p>
      <p className="mt-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {line}
      </p>
      <Bar used={used} requested={requested} total={total} />
      <Legend used={used} requested={requested} total={total} unit={unit} />
    </Panel>
  )
}

/**
 * Used, then the promised-but-idle remainder, then what is free.
 *
 * The middle band is the one worth having: it is capacity that exists, is not being used,
 * and cannot be scheduled — the reason a half-idle cluster refuses a deployment.
 */
function Bar({ used, requested, total }: { used: number; requested: number; total: number }) {
  const scale = total > 0 ? total : 1
  const usedPct = clamp((used / scale) * 100)
  const headroomPct = clamp((Math.max(requested - used, 0) / scale) * 100)

  return (
    <span
      className="mt-2.5 flex h-[7px] overflow-hidden rounded-[5px]"
      style={{ backgroundColor: 'var(--bg-inset, rgba(255,255,255,0.07))' }}
      role="img"
      aria-label={`${usedPct.toFixed(0)}% used, ${headroomPct.toFixed(0)}% requested but idle`}
    >
      <span style={{ width: `${usedPct}%`, backgroundColor: 'var(--accent)' }} />
      <span
        style={{
          width: `${headroomPct}%`,
          backgroundColor: 'var(--status-warn)',
          opacity: 0.55,
        }}
      />
    </span>
  )
}

function Legend({
  used,
  requested,
  total,
  unit,
  usedLabel = 'Used',
}: {
  used: number
  requested: number
  total: number
  unit: (value: number) => string
  usedLabel?: string
}) {
  const scale = total > 0 ? total : 1
  const free = Math.max(total - Math.max(requested, used), 0)

  const items: Array<[string, string, string]> = [
    [usedLabel, `${((used / scale) * 100).toFixed(0)}%`, 'var(--accent)'],
    [
      'Request headroom',
      `${((Math.max(requested - used, 0) / scale) * 100).toFixed(0)}%`,
      'var(--status-warn)',
    ],
    ['Free', `${((free / scale) * 100).toFixed(0)}%`, 'var(--text-muted)'],
  ]

  return (
    <span
      className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1"
      style={{ fontSize: '10px', color: 'var(--text-muted)' }}
    >
      {items.map(([label, value, colour]) => (
        <span key={label} className="flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className="h-1.5 w-1.5 rounded-full"
            style={{ backgroundColor: colour }}
          />
          {label} {value}
        </span>
      ))}
      <span className="ml-auto font-mono">{unit(total)} total</span>
    </span>
  )
}

const clamp = (v: number) => Math.max(0, Math.min(100, v))
const schedulable = (allocatable: number, requested: number) => Math.max(allocatable - requested, 0)

function sum<T>(rows: T[] | null | undefined, of: (row: T) => number): number {
  return (rows ?? []).reduce((total, row) => total + of(row), 0)
}

/** A percentage per node is only meaningful weighted by how big that node is. */
function weighted<T>(
  rows: T[] | null | undefined,
  percent: (row: T) => number,
  size: (row: T) => number,
): number {
  return (rows ?? []).reduce((total, row) => total + (percent(row) / 100) * size(row), 0)
}
