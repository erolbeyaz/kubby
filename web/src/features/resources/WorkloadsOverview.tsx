import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { ApiError, api, type ResourceRow, type WorkloadOverview } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

import { statusColor } from './statusColor'

interface WorkloadsOverviewProps {
  clusterId: string
  namespaces: string[]
  onOpenType: (typeKey: string) => void
}

/**
 * What is running, and what has been happening to it.
 *
 * The counts and the events are the same question at two distances. Read apart, a
 * rollout in progress looks like a cluster that is simply wrong; together, the bar that
 * is not full has a reason directly underneath it.
 */
export function WorkloadsOverview({ clusterId, namespaces, onOpenType }: WorkloadsOverviewProps) {
  const overview = useQuery({
    queryKey: ['workloads-overview', clusterId, namespaces.join(',')],
    queryFn: ({ signal }) => api.workloadsOverview(clusterId, namespaces, signal),
    refetchInterval: 20_000,
  })

  const error = overview.error instanceof ApiError ? overview.error : null
  if (error) {
    return (
      <div className="p-4">
        <Callout tone="error" title="Could not read this cluster" requestId={error.requestId}>
          {error.message}
        </Callout>
      </div>
    )
  }

  const counts = overview.data?.counts ?? []
  const events = overview.data?.events?.rows ?? []

  return (
    <div className="flex h-full flex-col">
      <div
        className="grid shrink-0 gap-x-8 gap-y-1.5 border-b px-4 py-3"
        style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(20rem, 1fr))', borderColor: 'var(--border-subtle)' }}
      >
        {counts.map((count) => (
          <CountBar key={count.typeKey} count={count} onOpen={() => onOpenType(count.typeKey)} />
        ))}
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        <EventTable rows={events} />
      </div>
    </div>
  )
}

function CountBar({
  count,
  onOpen,
}: {
  count: WorkloadOverview['counts'][number]
  onOpen: () => void
}) {
  const ratio = count.total === 0 ? 0 : count.ready / count.total
  const colour = ratio === 1 ? 'var(--status-ok)' : ratio === 0 ? 'var(--status-error)' : 'var(--status-warn)'

  return (
    <span className="flex items-center gap-3">
      <button
        type="button"
        onClick={onOpen}
        className="w-44 shrink-0 truncate text-left transition-colors hover:text-[var(--accent)]"
        style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--status-info)' }}
      >
        {count.kind}s ({count.total})
      </button>

      <span
        className="h-1 min-w-0 flex-1 overflow-hidden"
        style={{ backgroundColor: 'var(--bg-active)', borderRadius: 2 }}
      >
        <span
          className="block h-full"
          // An empty kind gets a full quiet bar rather than an empty alarming one: none
          // of nothing is broken.
          style={{
            width: count.total === 0 ? '100%' : `${ratio * 100}%`,
            backgroundColor: count.total === 0 ? 'var(--border-default)' : colour,
          }}
        />
      </span>
    </span>
  )
}

const EVENT_GRID = '5rem minmax(20rem, 3fr) 9rem 14rem 12rem 4rem 4rem 5rem'

function EventTable({ rows }: { rows: ResourceRow[] }) {
  if (rows.length === 0) {
    return (
      <p className="p-4" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-muted)' }}>
        No events. A quiet cluster is the good case.
      </p>
    )
  }

  return (
    <div>
      <div
        role="row"
        className="sticky top-0 z-10 grid items-center gap-3 border-b px-4"
        style={{
          gridTemplateColumns: EVENT_GRID,
          height: 32,
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-default)',
          fontSize: 'var(--text-secondary-size)',
          color: 'var(--text-muted)',
        }}
      >
        {['Type', 'Message', 'Namespace', 'Involved Object', 'Source', 'Count', 'Age', 'Last Seen'].map(
          (label) => (
            <span key={label} className="truncate">
              {label}
            </span>
          ),
        )}
      </div>

      {rows.map((row, index) => (
        <div
          key={`${row.namespace}/${row.name}/${index}`}
          className="grid items-center gap-3 border-b px-4"
          style={{
            gridTemplateColumns: EVENT_GRID,
            height: 32,
            borderColor: 'var(--border-subtle)',
            fontSize: 'var(--text-secondary-size)',
          }}
        >
          <span className="truncate" style={{ color: statusColor(row.fields['type'] ?? '') }}>
            {row.fields['type']}
          </span>
          <span
            className="truncate"
            style={{
              color: row.fields['type'] === 'Warning' ? 'var(--status-error)' : 'var(--text-primary)',
            }}
            title={row.fields['message']}
          >
            {row.fields['message']}
          </span>
          <span className="truncate" style={{ color: 'var(--text-secondary)' }}>
            {row.namespace}
          </span>
          <span className="truncate" style={{ color: 'var(--status-info)' }}>
            {row.fields['involvedObject']}
          </span>
          <span className="truncate font-mono" style={{ color: 'var(--text-secondary)' }}>
            {row.fields['source']}
          </span>
          <span className="font-mono" style={{ color: 'var(--text-secondary)' }}>
            {row.fields['count']}
          </span>
          <span className="font-mono" style={{ color: 'var(--text-muted)' }} title={formatAbsolute(row.createdAt)}>
            {row.age}
          </span>
          <span
            className="truncate font-mono"
            style={{ color: 'var(--text-muted)' }}
            title={row.fields['lastSeen'] ? formatAbsolute(row.fields['lastSeen']) : ''}
          >
            {row.fields['lastSeen'] ? formatAge(row.fields['lastSeen']) : '—'}
          </span>
        </div>
      ))}
    </div>
  )
}
