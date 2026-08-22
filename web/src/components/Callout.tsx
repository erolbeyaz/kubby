import type { ReactNode } from 'react'

type Tone = 'error' | 'warning' | 'info' | 'success'

const TONES: Record<Tone, string> = {
  error: 'var(--status-error)',
  warning: 'var(--status-warn)',
  info: 'var(--status-info)',
  success: 'var(--status-ok)',
}

interface CalloutProps {
  tone: Tone
  title?: string
  children: ReactNode
  requestId?: string | undefined
}

/**
 * Errors say what happened and carry the request id, so a user reporting a problem can
 * quote something that appears in the server log.
 */
export function Callout({ tone, title, children, requestId }: CalloutProps) {
  const color = TONES[tone]

  return (
    <div
      role={tone === 'error' ? 'alert' : 'status'}
      className="border px-3 py-2 text-[13px]"
      style={{
        borderColor: color,
        borderRadius: 'var(--radius-sharp)',
        backgroundColor: 'var(--bg-raised)',
        color: 'var(--text-secondary)',
      }}
    >
      {title && (
        <p className="mb-0.5 font-medium" style={{ color }}>
          {title}
        </p>
      )}
      <div>{children}</div>
      {requestId && (
        <p className="mt-1 font-mono text-[12px]" style={{ color: 'var(--text-muted)' }}>
          request {requestId}
        </p>
      )}
    </div>
  )
}
