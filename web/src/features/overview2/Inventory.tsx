import type { WorkloadOverview } from '@/lib/api'

import { RADIUS } from './parts'

/**
 * What this cluster is made of, directly under its name.
 *
 * The same counts the Workloads screen carries, in a form that survives being read at a
 * glance. There they are a wide two-column list whose bars run the width of the window —
 * a shape that makes the label and its bar so far apart that neither says much about the
 * other. Here each kind is one tile: the name, how many are ready out of how many exist,
 * and a short bar in that order, so the number is read and the bar only confirms it.
 *
 * Every tile opens its list. A count that cannot be clicked through is a count somebody
 * has to go and search for.
 */
export function Inventory({
  counts,
  onOpenType,
}: {
  counts: WorkloadOverview['counts']
  onOpenType: (typeKey: string) => void
}) {
  if (counts.length === 0) return null

  return (
    <div
      className="mb-3 grid gap-2"
      style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(8.5rem, 1fr))' }}
    >
      {counts.map((count) => (
        <Tile key={count.typeKey} count={count} onOpen={() => onOpenType(count.typeKey)} />
      ))}
    </div>
  )
}

function Tile({
  count,
  onOpen,
}: {
  count: WorkloadOverview['counts'][number]
  onOpen: () => void
}) {
  const empty = count.total === 0
  const ratio = empty ? 0 : count.ready / count.total
  // A kind with nothing in it is not a kind that is failing: it gets a quiet full bar
  // rather than an empty alarming one.
  const tone = empty
    ? 'var(--border-default)'
    : ratio === 1
      ? 'var(--status-ok)'
      : ratio === 0
        ? 'var(--status-error)'
        : 'var(--status-warn)'

  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex flex-col gap-1.5 border px-2.5 py-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
      style={{
        borderRadius: RADIUS,
        borderColor: 'var(--border-default)',
        backgroundColor: 'var(--bg-surface)',
      }}
      title={`${count.ready} of ${count.total} ${count.kind}s ready`}
    >
      <span className="truncate" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {count.kind}s
      </span>

      <span className="flex items-baseline gap-1">
        <span
          className="font-mono tabular-nums"
          style={{ fontSize: '17px', fontWeight: 500, color: 'var(--text-primary)' }}
        >
          {count.total}
        </span>
        {/* Only when it differs: "23 / 23" on every tile is a column of noise that hides
            the one tile where the two numbers are not the same. */}
        {!empty && count.ready !== count.total && (
          <span className="font-mono tabular-nums" style={{ fontSize: '11px', color: 'var(--status-warn)' }}>
            {count.ready} ready
          </span>
        )}
      </span>

      <span
        className="h-[3px] w-full overflow-hidden"
        style={{ backgroundColor: 'var(--bg-active)', borderRadius: 2 }}
      >
        <span
          className="block h-full"
          style={{ width: empty ? '100%' : `${ratio * 100}%`, backgroundColor: tone, borderRadius: 2 }}
        />
      </span>
    </button>
  )
}
