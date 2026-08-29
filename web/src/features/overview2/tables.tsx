import type { ReactNode } from 'react'

import type { ClusterAlert, LogFinding, PodProblem, Reading, StorageProblem, WorkloadRow } from '@/lib/api'

import { format, formatBytes } from '@/features/metrics/charts'

import { Pill, Quiet } from './parts'

/**
 * The tables of Overview 2.
 *
 * Each one is a real table with the columns the brief names, not a compressed list — the
 * whole reason these sections are tables is that the reader is comparing rows against each
 * other, and a list of name-and-figure cannot be compared down a column.
 *
 * Long tables scroll inside their own panel rather than pushing the rest of the page down.
 * A cluster with two hundred failing pods should still let somebody reach the control
 * plane section without a thousand rows of scrolling on the way.
 */
function Scroller({ children, rows }: { children: ReactNode; rows: number }) {
  // Roughly seven rows before it starts scrolling: enough to see a pattern, short enough
  // that the sections below stay reachable.
  const scrolls = rows > 7
  return (
    <div
      className="-mx-[13px] overflow-x-auto"
      style={scrolls ? { maxHeight: '19rem', overflowY: 'auto' } : undefined}
    >
      {children}
    </div>
  )
}

function Th({ children, align }: { children: ReactNode; align?: 'right' }) {
  return (
    <th
      className="sticky top-0 z-10 whitespace-nowrap px-[13px] py-2 font-semibold uppercase tracking-[0.06em]"
      style={{
        textAlign: align ?? 'left',
        fontSize: '10px',
        color: 'var(--text-muted)',
        // The header stays put while the body scrolls, so a column being read halfway
        // down the table still has a name.
        backgroundColor: 'var(--bg-surface)',
        boxShadow: 'inset 0 -1px 0 var(--border-subtle)',
      }}
    >
      {children}
    </th>
  )
}

function Td({
  children,
  align,
  tone,
  mono,
  top,
}: {
  children: ReactNode
  align?: 'right'
  tone?: string | undefined
  mono?: boolean
  top?: boolean
}) {
  return (
    <td
      className={`whitespace-nowrap px-[13px] py-2.5 ${mono ? 'font-mono tabular-nums' : ''} ${top ? 'align-top' : 'align-middle'}`}
      style={{ textAlign: align ?? 'left', color: tone ?? 'var(--text-secondary)' }}
    >
      {children}
    </td>
  )
}

function Row({
  children,
  onOpen,
}: {
  children: ReactNode
  onOpen?: (() => void) | undefined
}) {
  return (
    <tr
      className={onOpen ? 'cursor-pointer hover:bg-[var(--bg-hover)]' : undefined}
      onClick={onOpen}
      tabIndex={onOpen ? 0 : undefined}
      onKeyDown={
        onOpen
          ? (event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                onOpen()
              }
            }
          : undefined
      }
      style={{ borderTop: '1px solid var(--border-subtle)' }}
    >
      {children}
    </tr>
  )
}

/**
 * Problem pods — the table the whole screen is built around.
 *
 * Usage sits beside request and limit on purpose. The same CrashLoop reads differently at
 * 620m against a 500m request (a pod fighting for CPU) and at 24m against the same request
 * (an application falling over on its own), and only the three figures together say which.
 */
export function PodProblemTable({
  rows,
  onOpen,
}: {
  rows: PodProblem[]
  onOpen?: ((namespace: string, pod: string) => void) | undefined
}) {
  if (rows.length === 0) {
    return <Quiet>No pod is failing, pending or unready.</Quiet>
  }

  return (
    <Scroller rows={rows.length}>
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr>
            <Th>Namespace / Pod</Th>
            <Th>Status</Th>
            <Th>Reason</Th>
            <Th align="right">Restarts</Th>
            <Th>CPU u / r / l</Th>
            <Th>Memory u / r / l</Th>
            <Th align="right">Age</Th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <Row
              key={`${row.namespace}/${row.pod}`}
              onOpen={onOpen ? () => onOpen(row.namespace, row.pod) : undefined}
            >
              <Td top>
                <span className="block font-mono" style={{ color: 'var(--text-primary)' }}>
                  {row.namespace}/{row.pod}
                </span>
                <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
                  {row.node ?? '—'}
                </span>
              </Td>
              <Td top>
                <Pill tone={row.severity === 'error' ? 'bad' : 'warn'}>{row.status}</Pill>
              </Td>
              <Td top>{row.reason ?? '—'}</Td>
              <Td
                top
                align="right"
                mono
                tone={row.restarts > 0 ? 'var(--status-error)' : 'var(--text-muted)'}
              >
                {format(row.restarts)}
              </Td>
              <Td top mono>
                <Triple used={row.cpuUsed} request={row.cpuRequest} limit={row.cpuLimit} render={cores} />
              </Td>
              <Td top mono>
                <Triple
                  used={row.memoryUsed}
                  request={row.memoryRequest}
                  limit={row.memoryLimit}
                  render={formatBytes}
                />
              </Td>
              <Td top align="right" mono>
                {age(row.ageSeconds)}
              </Td>
            </Row>
          ))}
        </tbody>
      </table>
    </Scroller>
  )
}

/**
 * Used / requested / limit, with an em dash where there is no reading.
 *
 * A pending pod has requests and limits but no usage, and printing zero there would say
 * "this pod is using nothing" about a pod that was never started.
 */
function Triple({
  used,
  request,
  limit,
  render,
}: {
  used: Reading
  request: Reading
  limit: Reading
  render: (value: number) => string
}) {
  const show = (reading: Reading) =>
    reading.known ? render(reading.value) : <span style={{ color: 'var(--text-muted)' }}>—</span>

  return (
    <>
      <span style={{ color: 'var(--text-primary)' }}>{show(used)}</span>
      <span style={{ color: 'var(--text-muted)' }}> / </span>
      {show(request)}
      <span style={{ color: 'var(--text-muted)' }}> / </span>
      {show(limit)}
    </>
  )
}

/** Degraded workloads — what is short, by how much, and whether it is moving. */
/**
 * What the pods are saying about themselves.
 *
 * Ordered by how long it has been true rather than by how loud it is. A pod writing five
 * hundred lines a second for a minute is noisier than one that has been quietly failing
 * since yesterday, and the second is the one nobody has noticed.
 */
export function LogFindingTable({
  rows,
  onOpen,
}: {
  rows: LogFinding[]
  onOpen?: ((namespace: string, pod: string) => void) | undefined
}) {
  if (rows.length === 0) {
    return <Quiet>No pod is reporting an error in its own logs.</Quiet>
  }

  const ordered = [...rows].sort(
    (a, b) => Date.parse(a.firstSeen) - Date.parse(b.firstSeen) || b.count - a.count,
  )

  return (
    <Scroller rows={ordered.length}>
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr>
            <Th>Namespace / Pod</Th>
            <Th>Rule</Th>
            <Th align="right">Failing for</Th>
            <Th align="right">Lines</Th>
            <Th>What the log says</Th>
          </tr>
        </thead>
        <tbody>
          {ordered.map((row) => (
            <Row
              key={`${row.namespace}/${row.pod}/${row.rule}`}
              onOpen={onOpen ? () => onOpen(row.namespace, row.pod) : undefined}
            >
              <Td top>
                <span className="block font-mono" style={{ color: 'var(--text-primary)' }}>
                  {row.namespace}/{row.pod}
                </span>
                {row.pods && row.pods > 1 && (
                  <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
                    and {row.pods - 1} more pod{row.pods > 2 ? 's' : ''}
                  </span>
                )}
              </Td>
              <Td top>
                <Pill tone={row.severity === 'error' ? 'bad' : 'warn'}>{row.rule}</Pill>
                <span className="ml-1" style={{ color: 'var(--text-muted)' }}>
                  {row.class}
                </span>
              </Td>
              <Td top align="right">
                {failingFor(row)}
              </Td>
              <Td top align="right">
                {format(row.count)}
              </Td>
              <Td top>
                {row.summary && (
                  <span className="block font-mono" style={{ color: 'var(--text-primary)' }}>
                    {row.summary}
                  </span>
                )}
                <span className="block font-mono" style={{ color: 'var(--text-muted)' }}>
                  {row.sample}
                </span>
              </Td>
            </Row>
          ))}
        </tbody>
      </table>
    </Scroller>
  )
}

/** How long this has been true — the number that decides whether to look now. */
function failingFor(row: LogFinding): string {
  const span = Date.parse(row.lastSeen) - Date.parse(row.firstSeen)
  if (!Number.isFinite(span) || span < 60_000) return 'under a minute'

  const minutes = Math.round(span / 60_000)
  if (minutes < 60) return `${minutes}m`

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ${minutes % 60}m`
  return `${Math.floor(hours / 24)}d ${hours % 24}h`
}

export function DegradedTable({
  rows,
  scalers,
  onOpen,
}: {
  rows: WorkloadRow[]
  scalers: Array<{ namespace: string; name: string; current: number; max: number; atCeiling: boolean }>
  onOpen?: ((kind: string, namespace: string, name: string) => void) | undefined
}) {
  if (rows.length === 0) {
    return <Quiet>Every workload is at its desired replicas.</Quiet>
  }

  const scalerFor = (namespace: string, name: string) =>
    scalers.find((scaler) => scaler.namespace === namespace && scaler.name === name)

  return (
    <Scroller rows={rows.length}>
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr>
            <Th>Workload</Th>
            <Th>Kind</Th>
            <Th align="right">Ready / desired</Th>
            <Th align="right">Updated</Th>
            <Th>Status</Th>
            <Th>HPA</Th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const scaler = scalerFor(row.namespace, row.name)
            const none = row.ready === 0
            return (
              <Row
                key={`${row.kind}/${row.namespace}/${row.name}`}
                onOpen={onOpen ? () => onOpen(row.kind, row.namespace, row.name) : undefined}
              >
                <Td>
                  <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
                    {row.namespace}/{row.name}
                  </span>
                </Td>
                <Td>{row.kind}</Td>
                <Td align="right" mono tone={none ? 'var(--status-error)' : 'var(--status-warn)'}>
                  {format(row.ready)} / {format(row.desired)}
                </Td>
                <Td align="right" mono>
                  {format(row.updated)}
                </Td>
                <Td>
                  <Pill tone={none ? 'bad' : 'warn'}>
                    {none ? 'No replicas' : 'Degraded'}
                  </Pill>
                </Td>
                <Td tone={scaler?.atCeiling ? 'var(--status-warn)' : undefined}>
                  {scaler
                    ? `${format(scaler.current)} / ${format(scaler.max)}${scaler.atCeiling ? ' · at ceiling' : ''}`
                    : '—'}
                </Td>
              </Row>
            )
          })}
        </tbody>
      </table>
    </Scroller>
  )
}

/** Storage — claims and volumes that are not doing their job. */
export function StorageTable({
  rows,
  onOpen,
}: {
  rows: StorageProblem[]
  onOpen?: ((namespace: string, name: string) => void) | undefined
}) {
  if (rows.length === 0) {
    return <Quiet>Every claim is bound and every volume is healthy.</Quiet>
  }

  return (
    <Scroller rows={rows.length}>
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr>
            <Th>Namespace / PVC</Th>
            <Th>Status</Th>
            <Th align="right">Requested</Th>
            <Th align="right">In use</Th>
            <Th>StorageClass</Th>
            <Th>Health</Th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <Row
              key={`${row.namespace}/${row.name}`}
              onOpen={onOpen ? () => onOpen(row.namespace, row.name) : undefined}
            >
              <Td>
                <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
                  {row.namespace}/{row.name}
                </span>
              </Td>
              <Td>
                <Pill tone={row.phase === 'Lost' ? 'bad' : 'warn'}>{row.phase}</Pill>
              </Td>
              <Td align="right" mono>
                {row.capacityBytes.known ? formatBytes(row.capacityBytes.value) : '—'}
              </Td>
              <Td align="right" mono>
                {row.usedBytes.known ? (
                  formatBytes(row.usedBytes.value)
                ) : (
                  <span style={{ color: 'var(--text-muted)' }} title="Needs kubelet volume stats">
                    N/A
                  </span>
                )}
              </Td>
              <Td mono>{row.storageClass || '—'}</Td>
              <Td tone="var(--status-warn)">Not bound</Td>
            </Row>
          ))}
        </tbody>
      </table>
    </Scroller>
  )
}

/** Recent Kubernetes warning events, as the brief lays them out. */
export function EventTable({
  rows,
  failed,
}: {
  rows: Array<{ name: string; namespace?: string | undefined; age: string; fields?: Record<string, string> | undefined }>
  failed: boolean
}) {
  if (rows.length === 0) {
    return <Quiet>{failed ? 'Events could not be read.' : 'No warning events.'}</Quiet>
  }

  return (
    <Scroller rows={rows.length}>
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr>
            <Th align="right">Age</Th>
            <Th>Reason</Th>
            <Th>Object</Th>
            <Th>Message</Th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => {
            const warning = row.fields?.['type'] === 'Warning'
            return (
              <Row key={`${row.name}-${index}`}>
                <Td align="right" mono>
                  {row.age}
                </Td>
                <Td tone={warning ? 'var(--status-warn)' : undefined}>
                  {row.fields?.['reason'] ?? '—'}
                </Td>
                <Td mono>
                  {row.namespace ? `${row.namespace}/` : ''}
                  {row.fields?.['object'] ?? row.name}
                </Td>
                <Td>
                  <span
                    className="block max-w-[36rem] truncate"
                    title={row.fields?.['message'] ?? ''}
                    style={{ whiteSpace: 'normal' }}
                  >
                    {row.fields?.['message'] ?? ''}
                  </span>
                </Td>
              </Row>
            )
          })}
        </tbody>
      </table>
    </Scroller>
  )
}

/** Active alerts, as a table rather than a list — severity is a column worth sorting by. */
export function AlertTable({
  rows,
  known,
  onOpen,
}: {
  rows: ClusterAlert[]
  known: boolean
  onOpen?: ((kind: string, namespace: string, name: string) => void) | undefined
}) {
  if (!known) {
    return (
      <Quiet>
        No alerting rules are loaded in this Prometheus — nothing can fire, so this is N/A
        rather than zero.
      </Quiet>
    )
  }
  if (rows.length === 0) {
    return <Quiet>No alert is firing.</Quiet>
  }

  return (
    <Scroller rows={rows.length}>
      <table className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        <thead>
          <tr>
            <Th>Alert</Th>
            <Th>Severity</Th>
            <Th>Object</Th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <Row
              key={`${row.name}-${index}`}
              onOpen={
                onOpen && row.object
                  ? () => onOpen(row.kind ?? 'Pod', row.namespace ?? '', row.object ?? '')
                  : undefined
              }
            >
              <Td tone="var(--text-primary)">{row.name}</Td>
              <Td>
                <Pill tone={row.severity === 'critical' ? 'bad' : 'warn'}>
                  {row.severity || 'none'}
                </Pill>
              </Td>
              <Td mono>
                {row.object ? `${row.namespace ? `${row.namespace}/` : ''}${row.object}` : '—'}
              </Td>
            </Row>
          ))}
        </tbody>
      </table>
    </Scroller>
  )
}

const cores = (value: number) =>
  value < 1 ? `${Math.round(value * 1000)}m` : value.toFixed(2).replace(/\.00$/, '')

function age(seconds: number): string {
  if (seconds <= 0) return '—'
  if (seconds >= 86400) return `${Math.floor(seconds / 86400)}d`
  if (seconds >= 3600) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 60)}m`
}
