import type { NodeDetail } from '@/lib/api'

import { format, formatBytes } from '@/features/metrics/charts'

import { Pill } from './parts'

/**
 * One row per machine, with the columns the brief asks for.
 *
 * The CPU and memory columns read **used / requested / allocatable** rather than a single
 * percentage. The gap between the first two is the finding: a node at 3.2 used and 5.8
 * requested out of 7.5 is two-thirds idle and nearly full, and no single number says both.
 *
 * "Only nodes with problems" exists because a fleet of forty healthy machines is forty
 * rows nobody reads, and the one that is wrong is the one they came for.
 */
export function NodeHealthTable({
  nodes,
  problemsOnly,
  onOpen,
}: {
  nodes: NodeDetail[]
  problemsOnly: boolean
  onOpen?: ((name: string) => void) | undefined
}) {
  const shown = problemsOnly ? nodes.filter(hasProblem) : nodes

  if (nodes.length === 0) {
    return (
      <p className="p-3" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        No node metrics available.
      </p>
    )
  }

  if (shown.length === 0) {
    return (
      <p className="p-3" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        No node has a problem — {nodes.length} healthy, hidden by the filter.
      </p>
    )
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr style={{ color: 'var(--text-muted)' }}>
            <Th>Node</Th>
            <Th>Status</Th>
            <Th>CPU u / r / a</Th>
            <Th>Memory u / r / a</Th>
            <Th>Root disk</Th>
            <Th>Pod slots</Th>
            <Th>Pressure</Th>
            <Th>Network</Th>
            <Th>Agents</Th>
            <Th align="right">Uptime</Th>
          </tr>
        </thead>
        <tbody>
          {shown.map((node) => (
            <Row key={node.name} node={node} onOpen={onOpen} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Row({ node, onOpen }: { node: NodeDetail; onOpen?: ((name: string) => void) | undefined }) {
  const clickable = Boolean(onOpen)
  const podPercent = node.podCapacity > 0 ? (node.pods / node.podCapacity) * 100 : 0
  const errors = node.networkRxErrors + node.networkTxErrors

  return (
    <tr
      className={clickable ? 'cursor-pointer hover:bg-[var(--bg-hover)]' : undefined}
      onClick={clickable ? () => onOpen?.(node.name) : undefined}
      tabIndex={clickable ? 0 : undefined}
      onKeyDown={
        clickable
          ? (event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                onOpen?.(node.name)
              }
            }
          : undefined
      }
      style={{ borderTop: '1px solid var(--border-subtle)' }}
    >
      <Td>
        <span className="block font-mono" style={{ color: 'var(--text-primary)' }}>
          {node.name}
        </span>
        <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
          {node.role === 'control-plane' ? 'control-plane' : 'worker'}
          {node.kubeletVersion ? ` · ${node.kubeletVersion}` : ''}
        </span>
      </Td>

      <Td>
        <Pill tone={node.ready ? 'good' : 'bad'}>{node.ready ? 'Ready' : 'NotReady'}</Pill>
      </Td>

      <Metric
        used={(node.cpuPercent / 100) * node.cores}
        requested={(node.cpuCommittedPercent / 100) * (node.cpuAllocatable || node.cores)}
        allocatable={node.cpuAllocatable || node.cores}
        percent={node.cpuPercent}
        requestedPercent={node.cpuCommittedPercent}
        render={(v) => v.toFixed(1)}
        unit="cr"
      />

      <Metric
        used={(node.memoryPercent / 100) * node.memoryTotalBytes}
        requested={(node.memoryCommittedPercent / 100) * (node.memoryAllocatable || node.memoryTotalBytes)}
        allocatable={node.memoryAllocatable || node.memoryTotalBytes}
        percent={node.memoryPercent}
        requestedPercent={node.memoryCommittedPercent}
        render={(v) => formatBytes(v).replace(' ', '')}
        unit=""
      />

      <Td>
        <span style={{ color: tone(node.diskPercent, 80, 90) }}>{format(node.diskPercent)}%</span>
        <span className="block" style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
          inode {format(node.inodePercent)}%
        </span>
      </Td>

      <Td>
        <span style={{ color: tone(podPercent, 80, 95) }}>
          {format(node.pods)} / {format(node.podCapacity)}
        </span>
      </Td>

      <Td>
        <PressureCell node={node} />
      </Td>

      <Td>
        <span style={{ color: errors > 0 ? 'var(--status-warn)' : 'var(--text-secondary)' }}>
          {format(errors)} err
        </span>
        <span className="block" style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
          {format(node.networkDrops)} drop
        </span>
      </Td>

      <Td>
        <span className="flex flex-col gap-0.5">
          <Pill tone={node.nodeExporterUp ? 'good' : 'idle'}>
            {node.nodeExporterUp ? 'exporter' : 'no exporter'}
          </Pill>
          <Pill tone={node.kubeletUp ? 'good' : 'idle'}>
            {node.kubeletUp ? 'kubelet' : 'no kubelet'}
          </Pill>
        </span>
      </Td>

      <Td align="right">
        <span className="font-mono" style={{ color: 'var(--text-muted)' }}>
          {uptime(node.uptimeSeconds)}
        </span>
      </Td>
    </tr>
  )
}

/** Used and requested as two bands on one track, with the three figures beside them. */
function Metric({
  used,
  requested,
  allocatable,
  percent,
  requestedPercent,
  render,
  unit,
}: {
  used: number
  requested: number
  allocatable: number
  percent: number
  requestedPercent: number
  render: (value: number) => string
  unit: string
}) {
  return (
    <td className="px-2 py-1.5 align-top" style={{ minWidth: '9.5rem' }}>
      <span className="block whitespace-nowrap font-mono" style={{ color: 'var(--text-primary)' }}>
        {render(used)} / {render(requested)} / {render(allocatable)}
        {unit ? ` ${unit}` : ''}
      </span>
      <span
        className="mt-1 flex h-[5px] overflow-hidden rounded-[3px]"
        style={{ backgroundColor: 'var(--bg-inset, rgba(255,255,255,0.07))' }}
        title={`${percent.toFixed(0)}% used · ${requestedPercent.toFixed(0)}% requested`}
      >
        <span
          style={{
            width: `${Math.max(0, Math.min(100, percent))}%`,
            backgroundColor: tone(percent, 75, 90),
          }}
        />
        <span
          style={{
            width: `${Math.max(0, Math.min(100, Math.max(requestedPercent - percent, 0)))}%`,
            backgroundColor: 'var(--status-warn)',
            opacity: 0.45,
          }}
        />
      </span>
    </td>
  )
}

function PressureCell({ node }: { node: NodeDetail }) {
  const raised: string[] = []
  if (node.memoryPressure) raised.push('Memory')
  if (node.diskPressure) raised.push('Disk')
  if (node.pidPressure) raised.push('PID')
  if (node.networkUnavailable) raised.push('Network')
  if (node.unschedulable) raised.push('Cordoned')

  if (raised.length === 0) {
    return <Pill tone="good">None</Pill>
  }
  return (
    <span className="flex flex-wrap gap-1">
      {raised.map((name) => (
        <Pill key={name} tone="bad">
          {name}
        </Pill>
      ))}
    </span>
  )
}

function Th({ children, align }: { children: React.ReactNode; align?: 'right' }) {
  return (
    <th
      className="whitespace-nowrap px-2 py-1.5 font-semibold uppercase tracking-[0.06em]"
      style={{ textAlign: align ?? 'left', fontSize: '10px' }}
    >
      {children}
    </th>
  )
}

function Td({ children, align }: { children: React.ReactNode; align?: 'right' }) {
  return (
    <td
      className="whitespace-nowrap px-2 py-1.5 align-top"
      style={{ textAlign: align ?? 'left', color: 'var(--text-secondary)' }}
    >
      {children}
    </td>
  )
}

function hasProblem(node: NodeDetail): boolean {
  return (
    !node.ready ||
    node.memoryPressure ||
    node.diskPressure ||
    node.pidPressure ||
    node.networkUnavailable ||
    node.unschedulable ||
    !node.nodeExporterUp ||
    !node.kubeletUp ||
    node.diskPercent >= 80 ||
    node.inodePercent >= 80 ||
    node.cpuPercent >= 90 ||
    node.memoryPercent >= 90 ||
    node.networkRxErrors + node.networkTxErrors > 0
  )
}

function tone(percent: number, warn: number, error: number): string {
  if (percent >= error) return 'var(--status-error)'
  if (percent >= warn) return 'var(--status-warn)'
  return 'var(--status-ok)'
}

function uptime(seconds: number): string {
  if (seconds <= 0) return '—'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return days > 0 ? `${days}d ${hours}h` : `${hours}h`
}
