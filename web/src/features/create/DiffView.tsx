import type { ApplyResult } from '@/lib/api'

/**
 * What the server says it would do.
 *
 * Rendered per document, because a multi-document manifest can be half accepted, and
 * "the manifest failed" would hide which half.
 */
export function DiffView({ result }: { result: ApplyResult }) {
  return (
    <div className="flex flex-col">
      {result.results.map((entry, index) => (
        <section key={index} className="border-b" style={{ borderColor: 'var(--border-subtle)' }}>
          <h3
            className="flex items-baseline gap-1.5 px-3 py-1.5"
            style={{ fontSize: 'var(--text-micro)', backgroundColor: 'var(--bg-surface)' }}
          >
            <span style={{ color: 'var(--text-muted)' }}>{entry.kind}</span>
            <span style={{ color: 'var(--text-primary)' }}>
              {entry.namespace ? `${entry.namespace}/` : ''}
              {entry.name}
            </span>
            {entry.unchanged && (
              <span style={{ color: 'var(--text-muted)' }}>· no change</span>
            )}
          </h3>

          {entry.error && (
            <p className="px-3 py-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}>
              {entry.error}
            </p>
          )}

          {/* A change to a GitOps-managed object is reverted minutes later, and saying so
              before the apply is the difference between a decision and a surprise. */}
          {entry.owner && (
            <p
              className="px-3 py-1.5"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--status-warn)' }}
            >
              Managed by {entry.owner.controller}
              {entry.owner.instance ? ` (${entry.owner.instance})` : ''}.
              {entry.owner.selfHeal && ' Self-heal is on, so this change will be reverted.'}
            </p>
          )}

          {(entry.diff ?? []).length > 0 && (
            <pre
              className="overflow-x-auto px-3 py-2 font-mono"
              style={{ fontSize: 'var(--text-micro)', lineHeight: 1.55 }}
            >
              {(entry.diff ?? []).map((line, at) => (
                <div key={at} style={{ color: colourOf(line.kind), backgroundColor: groundOf(line.kind) }}>
                  {line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : ' '} {line.text}
                </div>
              ))}
            </pre>
          )}
        </section>
      ))}
    </div>
  )
}

function colourOf(kind: string): string {
  if (kind === 'added') return 'var(--status-ok)'
  if (kind === 'removed') return 'var(--status-error)'
  return 'var(--text-secondary)'
}

function groundOf(kind: string): string | undefined {
  if (kind === 'added') return 'color-mix(in srgb, var(--status-ok) 10%, transparent)'
  if (kind === 'removed') return 'color-mix(in srgb, var(--status-error) 10%, transparent)'
  return undefined
}
