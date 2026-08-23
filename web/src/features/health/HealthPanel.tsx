import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { ApiError, api, type Finding } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

import { CATEGORY_LABELS, SEVERITY_ORDER, severityColour } from './severity'

interface HealthPanelProps {
  clusterId: string
  namespaces: string[]
  onOpen: (finding: Finding) => void
}

/**
 * Everything wrong in a cluster, on one screen.
 *
 * The panel is grouped by category rather than listed flat: forty BackOff events and one
 * unschedulable pod are one theme and one theme, not forty-one rows, and a flat list
 * makes the rare finding the easiest to miss.
 */
export function HealthPanel({ clusterId, namespaces, onOpen }: HealthPanelProps) {
  const [severity, setSeverity] = useState<string | null>(null)

  const health = useQuery({
    queryKey: ['health', clusterId, namespaces.join(',')],
    queryFn: ({ signal }) => api.health(clusterId, namespaces, signal),
    refetchInterval: 30_000,
  })

  const error = health.error instanceof ApiError ? health.error : null
  const findings = health.data?.findings
  const grouped = useMemo(() => {
    const shown = severity ? (findings ?? []).filter((f) => f.severity === severity) : (findings ?? [])

    const groups = new Map<string, Finding[]>()
    for (const finding of shown) {
      const list = groups.get(finding.category) ?? []
      list.push(finding)
      groups.set(finding.category, list)
    }
    return [...groups.entries()]
  }, [findings, severity])

  if (error) {
    return (
      <div className="p-4">
        <Callout tone="error" title="Could not check this cluster" requestId={error.requestId}>
          {error.message}
        </Callout>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex h-12 shrink-0 items-center gap-2 border-b px-3"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        {SEVERITY_ORDER.map((level) => (
          <SeverityChip
            key={level}
            level={level}
            count={health.data?.counts[level] ?? 0}
            active={severity === level}
            onClick={() => setSeverity(severity === level ? null : level)}
          />
        ))}

        <span className="ml-auto font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {health.isFetching ? 'checking…' : `${findings?.length ?? 0} findings`}
        </span>
      </header>

      {/* A detector that could not run is said out loud. A user denied access to nodes
          should know their node checks did not happen, not read a quiet panel as health. */}
      {health.data?.failed && Object.keys(health.data.failed).length > 0 && (
        <div className="px-3 pt-3">
          <Callout tone="warning" title="Some checks could not run">
            {Object.entries(health.data.failed).map(([name, reason]) => (
              <span key={name} className="block">
                <strong>{name}</strong>: {reason}
              </span>
            ))}
          </Callout>
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-auto">
        {findings?.length === 0 && !health.isLoading && (
          <EmptyState title="Nothing is broken" description="No detector found a problem in this cluster." />
        )}

        {grouped.map(([category, items]) => (
          <section key={category}>
            <h3
              className="sticky top-0 z-10 border-b px-3 py-1.5 font-semibold uppercase"
              style={{
                fontSize: 'var(--text-micro)',
                letterSpacing: '0.08em',
                color: 'var(--text-muted)',
                borderColor: 'var(--border-subtle)',
                backgroundColor: 'var(--bg-surface)',
              }}
            >
              {CATEGORY_LABELS[category] ?? category} · {items.length}
            </h3>

            {items.map((finding, index) => (
              <FindingRow key={`${finding.name}/${finding.reason}/${index}`} finding={finding} onOpen={onOpen} />
            ))}
          </section>
        ))}
      </div>
    </div>
  )
}

function FindingRow({ finding, onOpen }: { finding: Finding; onOpen: (finding: Finding) => void }) {
  return (
    <button
      type="button"
      onClick={() => onOpen(finding)}
      className="flex w-full items-start gap-2 border-b px-3 py-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
      style={{ borderColor: 'var(--border-subtle)' }}
    >
      <span
        aria-label={finding.severity}
        className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full"
        style={{ backgroundColor: severityColour(finding.severity) }}
      />

      <span className="min-w-0 flex-1">
        <span className="flex items-baseline gap-1.5">
          <span
            className="font-mono"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
          >
            {finding.kind}
          </span>
          <span
            className="min-w-0 truncate"
            style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}
          >
            {finding.namespace ? `${finding.namespace}/` : ''}
            {finding.name}
          </span>
          {finding.container && (
            <span className="font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
              · {finding.container}
            </span>
          )}
        </span>

        <span className="mt-0.5 block" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>
          {finding.detail}
        </span>
      </span>

      <span className="flex shrink-0 flex-col items-end gap-0.5">
        <span
          className="px-1.5 font-mono"
          style={{
            fontSize: 'var(--text-micro)',
            borderRadius: 'var(--radius-sharp)',
            color: severityColour(finding.severity),
            backgroundColor: 'var(--bg-hover)',
          }}
        >
          {finding.reason}
        </span>
        {finding.count ? (
          <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>×{finding.count}</span>
        ) : null}
        {finding.lastSeen && (
          <span
            title={formatAbsolute(finding.lastSeen)}
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
          >
            {formatAge(finding.lastSeen)}
          </span>
        )}
      </span>
    </button>
  )
}

function SeverityChip({
  level,
  count,
  active,
  onClick,
}: {
  level: string
  count: number
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className="flex h-7 items-center gap-1.5 border px-2 transition-colors"
      style={{
        borderRadius: 'var(--radius-sharp)',
        fontSize: 'var(--text-micro)',
        borderColor: active ? severityColour(level) : 'var(--border-default)',
        backgroundColor: active ? 'var(--bg-hover)' : 'var(--bg-base)',
        color: count > 0 ? severityColour(level) : 'var(--text-muted)',
      }}
    >
      <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: severityColour(level) }} />
      {level}
      <span className="font-mono">{count}</span>
    </button>
  )
}
