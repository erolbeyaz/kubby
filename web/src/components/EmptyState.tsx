interface EmptyStateProps {
  title: string
  description: string
  hint?: string
}

/**
 * Empty is a real state, not an accident: it says why there is nothing here and what
 * the next action is (CONVENTIONS: loading / empty / error are handled separately).
 */
export function EmptyState({ title, description, hint }: EmptyStateProps) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
      <h3 className="text-[15px] font-medium" style={{ color: 'var(--text-secondary)' }}>
        {title}
      </h3>
      <p className="max-w-sm text-[13px]" style={{ color: 'var(--text-muted)' }}>
        {description}
      </p>
      {hint && (
        <p className="mt-1 font-mono text-[12px]" style={{ color: 'var(--text-muted)' }}>
          {hint}
        </p>
      )}
    </div>
  )
}
