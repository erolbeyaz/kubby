import type { ClusterHealthMetrics, NodeDetail } from '@/lib/api'

import { format } from '@/features/metrics/charts'

import { Pill, Quiet } from './parts'

/**
 * What is wrong with the machines, in words.
 *
 * This replaces a grid of green and red squares. That grid had two faults: nothing could
 * be read without hovering over it, and on a healthy cluster it was a wall of green that
 * took up a third of the screen to say "nothing". It also ignored the one fact worth
 * having — how long a condition has been true — which the server was already sending.
 *
 * A rollout raises DiskPressure for ninety seconds; a full disk raises it for two days.
 * Those are the same red square and completely different mornings.
 */
export function NodeConditions({
  issues,
  nodes,
}: {
  issues: NonNullable<ClusterHealthMetrics['nodeIssues']>
  nodes: NodeDetail[]
}) {
  // Conditions the node objects carry that the issue list does not: cordoning is a
  // decision somebody made, not a condition the kubelet raised, and it belongs here
  // because its effect is the same — nothing new will schedule there.
  const cordoned = nodes.filter((node) => node.unschedulable)
  const notReady = nodes.filter((node) => !node.ready)
  const blind = nodes.filter((node) => !node.nodeExporterUp || !node.kubeletUp)

  const rows = [
    ...notReady.map((node) => ({
      node: node.name,
      condition: 'NotReady',
      detail: 'the node is not accepting work',
      minutes: 0,
      tone: 'bad' as const,
    })),
    ...issues.map((issue) => ({
      node: issue.node,
      condition: issue.condition,
      detail: describe(issue.condition),
      minutes: issue.minutes,
      tone: 'bad' as const,
    })),
    ...cordoned.map((node) => ({
      node: node.name,
      condition: 'SchedulingDisabled',
      detail: 'cordoned — nothing new will land here',
      minutes: 0,
      tone: 'warn' as const,
    })),
    ...blind.map((node) => ({
      node: node.name,
      condition: node.nodeExporterUp ? 'KubeletNotScraped' : 'ExporterNotScraped',
      detail: 'this row of numbers may be stale',
      minutes: 0,
      tone: 'warn' as const,
    })),
  ]

  if (rows.length === 0) {
    return (
      <Quiet>
        All {nodes.length} node{nodes.length === 1 ? '' : 's'} are Ready, schedulable, and
        report no memory, disk, PID or network condition.
      </Quiet>
    )
  }

  // Longest-standing first: a condition that has been true for two days is not the same
  // finding as one raised a minute ago, and it is the one to look at.
  const sorted = [...rows].sort((a, b) => b.minutes - a.minutes)

  return (
    <div className="flex flex-col">
      {sorted.map((row) => (
        <span
          key={`${row.node}/${row.condition}`}
          className="grid items-center gap-3 border-b py-2 last:border-b-0"
          style={{ gridTemplateColumns: 'minmax(0,1fr) auto auto', borderColor: 'var(--border-subtle)' }}
        >
          <span className="min-w-0">
            <span
              className="block truncate font-mono"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-primary)' }}
            >
              {row.node}
            </span>
            <span className="block truncate" style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
              {row.detail}
            </span>
          </span>
          <Pill tone={row.tone}>{row.condition}</Pill>
          <span
            className="whitespace-nowrap font-mono tabular-nums"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
          >
            {row.minutes > 0 ? `for ${duration(row.minutes)}` : ''}
          </span>
        </span>
      ))}
      <p className="pt-2" style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
        {nodes.length - new Set(sorted.map((row) => row.node)).size} of {nodes.length} nodes are
        clear.
      </p>
    </div>
  )
}

function describe(condition: string): string {
  switch (condition) {
    case 'MemoryPressure':
      return 'the kubelet is reclaiming memory — pods will be evicted'
    case 'DiskPressure':
      return 'the node is running out of disk — images and logs will be culled'
    case 'PIDPressure':
      return 'too many processes — new containers will fail to start'
    case 'NetworkUnavailable':
      return 'the node network is not configured'
    default:
      return condition
  }
}

function duration(minutes: number): string {
  if (minutes >= 1440) return `${Math.floor(minutes / 1440)}d`
  if (minutes >= 60) return `${Math.floor(minutes / 60)}h`
  return `${Math.round(minutes)}m`
}

/**
 * Where each namespace is actually running.
 *
 * This replaces an opacity heatmap whose cells could only be read by hovering. The
 * question it exists to answer is not "how dense is this cell" but **"what would I lose if
 * one node went down"**, so it answers that directly: the namespaces whose pods are all on
 * one machine are listed first, with the machine named.
 *
 * A namespace on one node is not automatically wrong — a single-replica batch job belongs
 * on one node — so this reports rather than accuses, and says how many replicas are at
 * stake.
 */
export function Placement({
  spread,
  nodes,
}: {
  spread: NonNullable<ClusterHealthMetrics['spread']>
  nodes: string[]
}) {
  if (spread.length === 0) {
    return <Quiet>No pod placement is being reported.</Quiet>
  }

  const byNamespace = new Map<string, Map<string, number>>()
  for (const entry of spread) {
    const row = byNamespace.get(entry.namespace) ?? new Map<string, number>()
    row.set(entry.node, (row.get(entry.node) ?? 0) + entry.pods)
    byNamespace.set(entry.namespace, row)
  }

  const rows = [...byNamespace.entries()].map(([namespace, perNode]) => {
    const total = [...perNode.values()].reduce((sum, value) => sum + value, 0)
    const [busiestNode, busiestCount] = [...perNode.entries()].sort((a, b) => b[1] - a[1])[0] ?? ['', 0]
    return {
      namespace,
      perNode,
      total,
      nodesUsed: perNode.size,
      busiestNode,
      concentration: total > 0 ? busiestCount / total : 0,
    }
  })

  // Concentrated and large first — a namespace with twelve pods on one node is the answer
  // to the question; one with a single pod is not.
  const sorted = rows.sort((a, b) => {
    const risk = (row: (typeof rows)[number]) => (row.total > 1 ? row.concentration : 0)
    if (risk(b) !== risk(a)) return risk(b) - risk(a)
    return b.total - a.total
  })

  const atRisk = sorted.filter((row) => row.total > 1 && row.concentration === 1)

  // Colour follows the node, fixed by its position in the cluster's node list — never by
  // its rank in this table. A namespace that drops out must not repaint the rest.
  const colourOf = (node: string): string => {
    const slot = Math.max(nodes.indexOf(node), 0) % SERIES.length
    return SERIES[slot] ?? FALLBACK_SERIES
  }

  return (
    <div className="flex flex-col gap-2">
      <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>
        {atRisk.length === 0
          ? `Every namespace with more than one pod is spread across at least two of the ${nodes.length} nodes.`
          : `${atRisk.length} namespace${atRisk.length === 1 ? '' : 's'} would lose every pod if one node went down.`}
      </p>

      {/* The key comes before the bars, not after them. A colour you have to scroll past
          the chart to identify is a colour you read the chart without. */}
      <span
        className="flex flex-wrap items-center gap-x-3 gap-y-1 pb-1"
        style={{ fontSize: '10px', color: 'var(--text-secondary)' }}
      >
        {nodes.map((node) => (
          <span key={node} className="flex items-center gap-1.5">
            <Swatch colour={colourOf(node)} />
            <span className="font-mono">{node}</span>
          </span>
        ))}
      </span>

      <div className="flex flex-col">
        {sorted.slice(0, 8).map((row) => {
          const alone = row.total > 1 && row.concentration === 1
          // Biggest share first, so the bar and the counts under it read in the same
          // order and the eye does not have to match them up.
          const segments = nodes
            .map((node) => ({ node, count: row.perNode.get(node) ?? 0 }))
            .filter((segment) => segment.count > 0)
            .sort((a, b) => b.count - a.count)

          return (
            <span
              key={row.namespace}
              className="grid items-center gap-x-3 gap-y-1 border-b py-2 last:border-b-0"
              style={{
                gridTemplateColumns: 'minmax(7rem, 1fr) minmax(9rem, 2fr) auto',
                borderColor: 'var(--border-subtle)',
              }}
            >
              <span className="min-w-0">
                <span
                  className="block truncate font-mono"
                  style={{ fontSize: 'var(--text-micro)', color: 'var(--text-primary)' }}
                >
                  {row.namespace}
                </span>
                <span className="block truncate" style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
                  {alone
                    ? `all ${format(row.total)} pods on ${row.busiestNode}`
                    : `${format(row.total)} pods across ${row.nodesUsed} nodes`}
                </span>
              </span>

              {/*
                The bar carries the shape and the line under it carries the reading.
                The numbers used to sit inside the segments at nine pixels in the page's
                own background colour, which on a mid-tone fill is close to invisible and
                on a narrow segment was clipped away entirely. Outside, in text ink, they
                are the same size as every other number on the screen.
              */}
              <span className="min-w-0">
                <span className="flex h-2 gap-[2px]" role="img" aria-label={breakdown(segments)}>
                  {segments.map((segment) => (
                    <span
                      key={segment.node}
                      className="rounded-[2px]"
                      style={{
                        width: `${(segment.count / row.total) * 100}%`,
                        backgroundColor: colourOf(segment.node),
                      }}
                      title={`${segment.node}: ${segment.count} pods`}
                    />
                  ))}
                </span>

                <span
                  className="mt-1 flex flex-wrap items-center gap-x-2.5 gap-y-0.5"
                  style={{ fontSize: '10px', color: 'var(--text-secondary)' }}
                >
                  {segments.slice(0, NAMED).map((segment) => (
                    <span key={segment.node} className="flex items-center gap-1">
                      <Swatch colour={colourOf(segment.node)} />
                      <span className="font-mono">{shortNode(segment.node)}</span>
                      <span className="font-mono tabular-nums" style={{ color: 'var(--text-primary)' }}>
                        {segment.count}
                      </span>
                    </span>
                  ))}
                  {/* On a wide cluster the tail would wrap this row onto three lines to
                      name machines holding a pod each. The bar still draws them and the
                      label on it still names them. */}
                  {segments.length > NAMED && (
                    <span style={{ color: 'var(--text-muted)' }}>
                      +{segments.length - NAMED} more
                    </span>
                  )}
                </span>
              </span>

              <span className="whitespace-nowrap">
                {alone ? (
                  <Pill tone="warn">One node</Pill>
                ) : (
                  <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
                    {Math.round(row.concentration * 100)}% on one
                  </span>
                )}
              </span>
            </span>
          )
        })}
      </div>
    </div>
  )
}

function Swatch({ colour }: { colour: string }) {
  return (
    <span
      aria-hidden="true"
      className="h-2 w-2 shrink-0 rounded-[2px]"
      style={{ backgroundColor: colour }}
    />
  )
}

/** The bar is a picture; this is what it says, for anyone not looking at it. */
function breakdown(segments: Array<{ node: string; count: number }>): string {
  return segments.map((segment) => `${segment.node}: ${segment.count} pods`).join(', ')
}

/** Node names in a cluster share a long prefix; the tail is the part that differs. */
function shortNode(name: string): string {
  const parts = name.split('-')
  return parts.length > 2 ? parts.slice(-2).join('-') : name
}

/*
 * Identity, in a fixed order that is never cycled and never reassigned.
 *
 * These replace five mixes of the accent and the status colours. Two of those were near
 * enough to each other to be one colour, and the other three borrowed green, amber and
 * red from the status palette — so a node segment said "warning" when all it meant was
 * "the second machine". The tokens behind these are validated as a set in theme.css.
 */
/** Named counts per row before the tail is summarised. */
const NAMED = 5

const FALLBACK_SERIES = 'var(--series-1)'

const SERIES = [
  'var(--series-1)',
  'var(--series-2)',
  'var(--series-3)',
  'var(--series-4)',
  'var(--series-5)',
  'var(--series-6)',
]
