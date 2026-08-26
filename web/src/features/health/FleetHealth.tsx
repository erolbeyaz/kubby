import { useQuery } from '@tanstack/react-query'

import { ApiError, api, type ClusterCard } from '@/lib/api'

import { severityColour } from './severity'

interface FleetHealthProps {
  onOpen: (clusterId: string) => void
  /** Marked as where the reader currently is. */
  currentId?: string
}

/**
 * The rest of the fleet, as a strip across the top of one cluster's overview.
 *
 * Reading one cluster with the others out of view is how a fleet-wide outage reads as a
 * single cluster's problem. The full-page version of this is the Home screen; there is
 * one of those, not two.
 */
export function FleetHealth({ onOpen, currentId }: FleetHealthProps) {
  const fleet = useQuery({
    queryKey: ['fleet-health'],
    queryFn: ({ signal }) => api.fleetHealth(signal),
    refetchInterval: 60_000,
  })

  const error = fleet.error instanceof ApiError ? fleet.error : null
  const clusters = fleet.data?.clusters ?? []

  // A strip that cannot load says nothing rather than pushing an error banner above the
  // cluster the reader actually opened. One cluster on its own is not a fleet.
  if (error || clusters.length < 2) return null

  return (
    <section aria-label="Fleet" className="flex gap-2 overflow-x-auto pb-1">
      {clusters.map((card) => (
        <Card key={card.id} card={card} current={card.id === currentId} onOpen={() => onOpen(card.id)} />
      ))}
    </section>
  )
}

function Card({
  card,
  onOpen,
  current = false,
}: {
  card: ClusterCard
  onOpen: () => void
  current?: boolean
}) {
  const critical = card.counts['critical'] ?? 0
  const warning = card.counts['warning'] ?? 0

  const body = (
    <>
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
        <>
          {/* Said in words, the same as on Home: the strip is narrow, and a grey edge on
              its own does not distinguish "quiet" from "nobody can reach it". */}
          <span
            className="mt-2 flex items-center gap-1.5"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}
          >
            <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ backgroundColor: 'var(--status-error)' }} />
            Not connected
          </span>
          {/* Truncated here rather than wrapped: a card in a strip cannot grow to fit a
              DNS error, and the whole text is on Home. */}
          <p
            className="mt-0.5 truncate"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            title={card.error || 'Could not be reached.'}
          >
            {card.error || 'Could not be reached.'}
          </p>
        </>
      ) : (
        <div className="mt-2 flex items-center gap-3">
          <Count label="critical" value={critical} />
          <Count label="warning" value={warning} />
          {critical === 0 && warning === 0 && (
            <span style={{ fontSize: 'var(--text-micro)', color: 'var(--status-ok)' }}>Healthy</span>
          )}
        </div>
      )}
    </>
  )

  const style = {
    borderRadius: 'var(--radius-panel)',
    // The card's left edge carries the verdict, so a wall of cards reads at a glance.
    borderColor: current ? 'var(--accent)' : 'var(--border-default)',
    borderLeftWidth: 3,
    borderLeftColor: card.unreachable
      ? 'var(--status-error)'
      : critical > 0
        ? 'var(--status-error)'
        : warning > 0
          ? 'var(--status-warn)'
          : 'var(--status-ok)',
  }

  // Not a button. There is nothing behind a cluster nobody can reach, and offering the
  // click is how somebody lands on an error page wondering how to get back — the same
  // rule the Home cards follow (ADR-125).
  if (card.unreachable) {
    return (
      <div
        className="flex w-52 shrink-0 flex-col border bg-[var(--bg-surface)] p-2 text-left"
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
      aria-current={current ? 'true' : undefined}
      className="flex w-52 shrink-0 flex-col border bg-[var(--bg-surface)] p-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
      style={style}
    >
      {body}
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
