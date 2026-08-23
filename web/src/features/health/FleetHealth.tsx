import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { ApiError, api, type ClusterCard } from '@/lib/api'
import { formatAge } from '@/lib/time'

import { severityColour } from './severity'

interface FleetHealthProps {
  onOpen: (clusterId: string) => void
  /**
   * A strip rather than a page, for the top of a cluster's own overview.
   *
   * Reading one cluster without the rest in view is how a fleet-wide outage looks like a
   * single cluster's problem.
   */
  compact?: boolean
  /** Marked as where the reader currently is. */
  currentId?: string
}

/**
 * Every cluster's health on one screen.
 *
 * This is the question people arrive with. Not "is this cluster broken" — they do not
 * know which cluster to open yet, which is the whole reason for opening the tool.
 */
export function FleetHealth({ onOpen, compact = false, currentId }: FleetHealthProps) {
  const fleet = useQuery({
    queryKey: ['fleet-health'],
    queryFn: ({ signal }) => api.fleetHealth(signal),
    refetchInterval: 60_000,
  })

  const error = fleet.error instanceof ApiError ? fleet.error : null
  const clusters = fleet.data?.clusters ?? []

  if (error) {
    // A strip that cannot load says nothing rather than pushing an error banner above
    // the cluster the reader actually opened.
    if (compact) return null

    return (
      <div className="p-4">
        <Callout tone="error" title="Could not check your clusters" requestId={error.requestId}>
          {error.message}
        </Callout>
      </div>
    )
  }

  if (compact) {
    if (clusters.length < 2) return null

    return (
      <section aria-label="Fleet" className="flex gap-2 overflow-x-auto pb-1">
        {clusters.map((card) => (
          <Card key={card.id} card={card} current={card.id === currentId} compact onOpen={() => onOpen(card.id)} />
        ))}
      </section>
    )
  }

  return (
    <div className="h-full overflow-auto p-4">
      <header className="mb-3 flex items-baseline gap-2">
        <h1 className="font-semibold" style={{ fontSize: 'var(--text-title)', color: 'var(--text-primary)' }}>
          Fleet health
        </h1>
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {fleet.isFetching ? 'checking…' : `${clusters.length} clusters`}
        </span>
      </header>

      {clusters.length === 0 && !fleet.isLoading && (
        <EmptyState title="No clusters yet" description="Add a cluster to see its health here." />
      )}

      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(19rem, 1fr))' }}>
        {clusters.map((card) => (
          <Card key={card.id} card={card} onOpen={() => onOpen(card.id)} />
        ))}
      </div>
    </div>
  )
}

function Card({
  card,
  onOpen,
  compact = false,
  current = false,
}: {
  card: ClusterCard
  onOpen: () => void
  compact?: boolean
  current?: boolean
}) {
  const critical = card.counts['critical'] ?? 0
  const warning = card.counts['warning'] ?? 0

  return (
    <button
      type="button"
      onClick={onOpen}
      aria-current={current ? 'true' : undefined}
      className={`flex flex-col border bg-[var(--bg-surface)] text-left transition-colors hover:bg-[var(--bg-hover)] ${
        compact ? 'w-52 shrink-0 p-2' : 'p-3'
      }`}
      style={{
        borderRadius: 'var(--radius-panel)',
        // The card's left edge carries the verdict, so a wall of cards reads at a glance.
        borderColor: current ? 'var(--accent)' : 'var(--border-default)',
        borderLeftWidth: 3,
        borderLeftColor: card.unreachable
          ? 'var(--text-muted)'
          : critical > 0
            ? 'var(--status-error)'
            : warning > 0
              ? 'var(--status-warn)'
              : 'var(--status-ok)',
      }}
    >
      <div className="flex items-center gap-2">
        {card.colour && (
          <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: card.colour }} />
        )}
        <span
          className="min-w-0 flex-1 truncate font-semibold"
          style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}
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

      {card.unreachable ? (
        <p className="mt-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {card.error || 'Could not be reached.'}
        </p>
      ) : (
        <>
          <div className="mt-2 flex items-center gap-3">
            <Count label="critical" value={critical} />
            <Count label="warning" value={warning} />
            {critical === 0 && warning === 0 && (
              <span style={{ fontSize: 'var(--text-micro)', color: 'var(--status-ok)' }}>Healthy</span>
            )}
          </div>

          {!compact &&
            (card.top ?? []).map((finding, index) => (
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

      {!compact && (
        <p className="mt-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          checked {formatAge(card.checkedAt)} ago
          {card.stale && ' · cached'}
        </p>
      )}
    </button>
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
