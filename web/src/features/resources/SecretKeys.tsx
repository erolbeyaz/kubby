import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { CopyButton } from '@/components/CopyButton'
import { ApiError, api } from '@/lib/api'

interface SecretKeysProps {
  clusterId: string
  namespace: string
  name: string
}

/**
 * A secret's keys, with each value revealed only when asked for.
 *
 * Masked is the default and disclosure is one key at a time: there is no "show
 * everything", because each reveal is its own decision and leaves its own audit record
 * on the server (ADR-057). Revealed values live in component state and nowhere else —
 * not the query cache, not storage — so closing the panel forgets them.
 */
export function SecretKeys({ clusterId, namespace, name }: SecretKeysProps) {
  const [revealed, setRevealed] = useState<Record<string, string>>({})
  const [pending, setPending] = useState<string | null>(null)
  const [failed, setFailed] = useState<Record<string, string>>({})

  const keys = useQuery({
    queryKey: ['secret-keys', clusterId, namespace, name],
    queryFn: ({ signal }) => api.secretKeys(clusterId, namespace, name, signal),
  })

  const toggle = async (key: string) => {
    if (key in revealed) {
      setRevealed((current) => omit(current, key))
      return
    }

    setPending(key)
    try {
      const result = await api.revealSecret(clusterId, namespace, name, key)
      setRevealed((current) => ({ ...current, [key]: result.value }))
      setFailed((current) => omit(current, key))
    } catch (error) {
      const message = error instanceof ApiError ? error.message : 'Could not read this key.'
      setFailed((current) => ({ ...current, [key]: message }))
    } finally {
      setPending(null)
    }
  }

  const error = keys.error instanceof ApiError ? keys.error : null
  if (error) {
    return (
      <p className="px-3 py-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}>
        {error.message}
      </p>
    )
  }

  return (
    <div className="flex flex-col">
      {(keys.data?.keys ?? []).map(({ key, bytes }) => {
        const value = revealed[key]
        const isOpen = value !== undefined

        return (
          <div key={key} className="border-b px-3 py-1.5" style={{ borderColor: 'var(--border-subtle)' }}>
            <div className="flex items-center gap-2">
              <span
                className="min-w-0 flex-1 truncate font-mono"
                style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
              >
                {key}
              </span>

              <span className="font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                {bytes} B
              </span>

              <button
                type="button"
                onClick={() => void toggle(key)}
                disabled={pending === key}
                aria-label={isOpen ? `Hide ${key}` : `Reveal ${key}`}
                aria-pressed={isOpen}
                title={isOpen ? 'Hide' : 'Reveal — this is recorded in the audit log'}
                className="flex h-6 w-6 items-center justify-center transition-colors hover:bg-[var(--bg-hover)] disabled:opacity-40"
                style={{ borderRadius: 'var(--radius-sharp)', color: isOpen ? 'var(--accent)' : 'var(--text-muted)' }}
              >
                <EyeIcon open={isOpen} />
              </button>

              {isOpen && <CopyButton value={value} label="Copy" />}
            </div>

            {isOpen && (
              <pre
                className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-sm px-2 py-1 font-mono"
                style={{
                  fontSize: 'var(--text-micro)',
                  backgroundColor: 'var(--bg-base)',
                  color: 'var(--text-primary)',
                }}
              >
                {value}
              </pre>
            )}

            {failed[key] && (
              <p className="mt-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}>
                {failed[key]}
              </p>
            )}
          </div>
        )
      })}

      {keys.data?.keys.length === 0 && (
        <p className="px-3 py-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          This secret has no data.
        </p>
      )}
    </div>
  )
}

/** Drops one key, so hiding a value removes it from state rather than blanking it. */
function omit(values: Record<string, string>, key: string): Record<string, string> {
  const next = { ...values }
  delete next[key]
  return next
}

function EyeIcon({ open }: { open: boolean }) {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3" aria-hidden="true">
      <path d="M1.5 8s2.4-4 6.5-4 6.5 4 6.5 4-2.4 4-6.5 4-6.5-4-6.5-4Z" />
      <circle cx="8" cy="8" r="1.9" />
      {!open && <path d="M2.5 2.5 13.5 13.5" strokeLinecap="round" />}
    </svg>
  )
}
