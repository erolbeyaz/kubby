import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { Icon } from '@/components/Icon'
import { ApiError, api, type ClusterCard } from '@/lib/api'
import { formatAge } from '@/lib/time'

import { severityColour } from '@/features/health/severity'

/**
 * Every cluster, and whether it can be reached.
 *
 * This is the question people arrive with. Not "is this cluster broken" — they do not
 * know which cluster to open yet, which is the whole reason for opening the tool.
 *
 * The card has to say what is on the other end of the link *before* anyone clicks it:
 * whether Kubby is connected, how many machines are there and how big they are. A card
 * that cannot be reached is not a button. Opening a cluster nobody can talk to lands the
 * reader on an error page with no way back to here, which is exactly the trap this screen
 * exists to prevent.
 */
export function Home({ onOpen }: { onOpen: (clusterId: string) => void }) {
  const fleet = useQuery({
    queryKey: ['fleet-health'],
    queryFn: ({ signal }) => api.fleetHealth(signal),
    refetchInterval: 60_000,
  })

  const error = fleet.error instanceof ApiError ? fleet.error : null
  const clusters = fleet.data?.clusters ?? []
  const reachable = clusters.filter((card) => !card.unreachable).length

  if (error) {
    return (
      <div className="p-4">
        <Callout tone="error" title="Could not check your clusters" requestId={error.requestId}>
          {error.message}
        </Callout>
      </div>
    )
  }

  return (
    <div className="h-full overflow-auto p-4">
      <header className="mb-3 flex items-baseline gap-2">
        <h1 className="font-semibold" style={{ fontSize: 'var(--text-title)', color: 'var(--text-primary)' }}>
          Clusters
        </h1>
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {fleet.isFetching && clusters.length === 0
            ? 'checking…'
            : `${reachable} of ${clusters.length} connected`}
        </span>
      </header>

      {clusters.length === 0 && !fleet.isLoading && (
        <EmptyState title="No clusters yet" description="Add a cluster to see it here." />
      )}

      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(23rem, 1fr))' }}>
        {clusters.map((card) => (
          <Card key={card.id} card={card} onOpen={() => onOpen(card.id)} />
        ))}
      </div>
    </div>
  )
}

function Card({ card, onOpen }: { card: ClusterCard; onOpen: () => void }) {
  const critical = card.counts['critical'] ?? 0
  const warning = card.counts['warning'] ?? 0
  const capacity = card.capacity

  // The left edge carries the verdict, so a wall of cards reads at a glance.
  const edge = card.unreachable
    ? 'var(--status-error)'
    : critical > 0
      ? 'var(--status-error)'
      : warning > 0
        ? 'var(--status-warn)'
        : 'var(--status-ok)'

  const body = (
    <>
      <div className="flex items-center gap-2">
        {card.colour && (
          <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: card.colour }} />
        )}
        <span
          className="min-w-0 flex-1 truncate font-semibold"
          style={{ fontSize: 'var(--text-body)', color: 'var(--text-primary)' }}
        >
          {card.name}
        </span>
        {card.environment && (
          <span
            className="px-1.5 font-mono uppercase"
            style={{
              fontSize: 'var(--text-micro)',
              borderRadius: 'var(--radius-sharp)',
              backgroundColor: 'var(--bg-hover)',
              color: 'var(--text-muted)',
            }}
          >
            {card.environment}
          </span>
        )}
      </div>

      {/* Connected or not, in words as well as colour, because this is the one fact the
          card exists to carry. */}
      <div className="mt-2 flex items-center gap-2">
        <span
          aria-hidden="true"
          className="h-2 w-2 shrink-0 rounded-full"
          style={{ backgroundColor: card.unreachable ? 'var(--status-error)' : 'var(--status-ok)' }}
        />
        <span
          style={{
            fontSize: 'var(--text-secondary-size)',
            color: card.unreachable ? 'var(--status-error)' : 'var(--status-ok)',
          }}
        >
          {card.unreachable ? 'Not connected' : 'Connected'}
        </span>
        {capacity?.k8sVersion && (
          <span className="ml-auto font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            {capacity.k8sVersion}
          </span>
        )}
      </div>

      {card.unreachable ? (
        <>
          <p
            className="mt-2 break-words font-mono"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
          >
            {card.error || 'Could not be reached.'}
          </p>
          <p className="mt-2 flex items-center gap-1.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            <Icon name="warning" />
            Cannot be opened until it answers again
          </p>
        </>
      ) : (
        <>
          {/* What is on the other end of the link. */}
          <div
            className="mt-3 grid gap-2 border-t pt-3"
            style={{ gridTemplateColumns: 'repeat(4, minmax(0, 1fr))', borderColor: 'var(--border-subtle)' }}
          >
            <Fact
              label="nodes"
              value={capacity ? String(capacity.nodes) : '—'}
              sub={
                capacity && capacity.nodesReady < capacity.nodes
                  ? `${capacity.nodesReady} ready`
                  : undefined
              }
              tone={
                capacity && capacity.nodesReady < capacity.nodes ? 'var(--status-error)' : undefined
              }
            />
            <Fact label="cores" value={capacity ? formatCores(capacity.cores) : '—'} />
            <Fact label="memory" value={capacity ? formatMiB(capacity.memoryMiB) : '—'} />
            <Fact label="pod slots" value={capacity ? String(capacity.pods) : '—'} />
          </div>

          <div className="mt-3 flex items-center gap-4 border-t pt-3" style={{ borderColor: 'var(--border-subtle)' }}>
            {critical === 0 && warning === 0 ? (
              <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--status-ok)' }}>
                Nothing needs attention
              </span>
            ) : (
              <>
                <Count label="critical" value={critical} />
                <Count label="warning" value={warning} />
              </>
            )}
          </div>

          {(card.top ?? []).slice(0, 2).map((finding, index) => (
            <p
              key={index}
              className="mt-1 truncate"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
            >
              <span style={{ color: severityColour(finding.severity) }}>{finding.reason}</span>
              {' · '}
              {finding.namespace ? `${finding.namespace}/` : ''}
              {finding.name}
            </p>
          ))}
        </>
      )}

      <p className="mt-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        checked {formatAge(card.checkedAt)} ago
        {card.stale && ' · cached'}
      </p>
    </>
  )

  const style = {
    borderRadius: 'var(--radius-panel)',
    borderColor: 'var(--border-default)',
    borderLeftWidth: 3,
    borderLeftColor: edge,
  }

  // Not a button. A card that cannot be reached has nothing behind it, and offering the
  // click is how somebody ends up on an error page wondering how to get back.
  if (card.unreachable) {
    return (
      <div
        className="flex flex-col border bg-[var(--bg-surface)] p-3 text-left"
        style={{ ...style, opacity: 0.75 }}
      >
        {body}
      </div>
    )
  }

  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex flex-col border bg-[var(--bg-surface)] p-3 text-left transition-colors hover:bg-[var(--bg-hover)]"
      style={style}
    >
      {body}
    </button>
  )
}

function Fact({
  label,
  value,
  sub,
  tone,
}: {
  label: string
  value: string
  sub?: string | undefined
  tone?: string | undefined
}) {
  return (
    <span className="flex min-w-0 flex-col">
      <span
        className="truncate font-mono tabular-nums"
        style={{ fontSize: '17px', fontWeight: 500, color: tone ?? 'var(--text-primary)' }}
      >
        {value}
      </span>
      <span className="truncate" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {sub ?? label}
      </span>
    </span>
  )
}

function Count({ label, value }: { label: string; value: number }) {
  return (
    <span className="flex items-baseline gap-1">
      <span className="font-mono" style={{ fontSize: 'var(--text-title)', color: severityColour(label) }}>
        {value}
      </span>
      <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>{label}</span>
    </span>
  )
}

const formatCores = (value: number) => (Number.isInteger(value) ? String(value) : value.toFixed(1))

function formatMiB(value: number): string {
  if (value >= 1024) return `${(value / 1024).toFixed(1)} GiB`
  return `${Math.round(value)} MiB`
}
