import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { CopyButton } from '@/components/CopyButton'
import { ApiError, api } from '@/lib/api'

interface DescribePaneProps {
  clusterId: string
  typeKey: string
  namespace: string
  name: string
}

/**
 * kubectl describe, coloured.
 *
 * The text is kubectl's own, so it is the output people already know how to read; the
 * only thing added is colour, because a wall of aligned monospace is where the one line
 * that matters hides.
 */
export function DescribePane({ clusterId, typeKey, namespace, name }: DescribePaneProps) {
  const described = useQuery({
    queryKey: ['describe', clusterId, typeKey, namespace, name],
    queryFn: ({ signal }) =>
      api.describe(clusterId, typeKey, { name, ...(namespace ? { namespace } : {}) }, signal),
  })

  const error = described.error instanceof ApiError ? described.error : null
  const text = described.data?.text ?? ''
  const lines = useMemo(() => text.split('\n'), [text])

  if (error) {
    return (
      <div className="p-3">
        <Callout tone="error" title="Could not describe" requestId={error.requestId}>
          {error.message}
        </Callout>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col" style={{ backgroundColor: 'var(--bg-base)' }}>
      <header
        className="flex h-9 shrink-0 items-center justify-end gap-2 border-b px-2"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        <CopyButton value={text} label="Copy" />
      </header>

      <div
        className="min-h-0 flex-1 overflow-auto px-3 py-2 font-mono"
        style={{ fontSize: 'var(--text-micro)', lineHeight: 1.6 }}
      >
        {described.isLoading && <p style={{ color: 'var(--text-muted)' }}>Describing…</p>}
        {lines.map((line, index) => (
          <DescribeLine key={index} line={line} />
        ))}
      </div>
    </div>
  )
}

const SECTION = /^([A-Z][A-Za-z ]*):(\s*)(.*)$/
const FIELD = /^(\s+)([A-Za-z][A-Za-z0-9 /()._-]*):(\s*)(.*)$/

function DescribeLine({ line }: { line: string }) {
  const section = SECTION.exec(line)
  if (section) {
    const [, label, gap, value] = section
    return (
      <div className="whitespace-pre-wrap">
        <span style={{ color: 'var(--accent)', fontWeight: 600 }}>{label}:</span>
        {gap}
        <span style={{ color: valueColour(value ?? '') }}>{value}</span>
      </div>
    )
  }

  const field = FIELD.exec(line)
  if (field) {
    const [, indent, label, gap, value] = field
    return (
      <div className="whitespace-pre-wrap">
        {indent}
        <span style={{ color: 'var(--text-muted)' }}>{label}:</span>
        {gap}
        <span style={{ color: valueColour(value ?? '') }}>{value}</span>
      </div>
    )
  }

  return (
    <div className="whitespace-pre-wrap" style={{ color: valueColour(line) }}>
      {line}
    </div>
  )
}

// The status words are what the eye is looking for in a describe, so they are the only
// values that get a colour of their own.
const BAD = /\b(Failed|Error|Unhealthy|CrashLoopBackOff|ImagePullBackOff|ErrImagePull|OOMKilled|Evicted|NotReady|Unschedulable|BackOff|Warning)\b/
const GOOD = /\b(Running|Ready|True|Succeeded|Bound|Active|Healthy|Normal|Completed)\b/
const WAITING = /\b(Pending|Waiting|Terminating|Unknown|False|ContainerCreating|PodInitializing)\b/

function valueColour(value: string): string {
  if (BAD.test(value)) return 'var(--status-error)'
  if (WAITING.test(value)) return 'var(--status-warn)'
  if (GOOD.test(value)) return 'var(--status-ok)'
  return 'var(--text-secondary)'
}
