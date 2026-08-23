import { useEffect, useRef, useState } from 'react'

interface CopyButtonProps {
  value: string
  label?: string
}

/**
 * Copies text to the clipboard and says so.
 *
 * Silent success is indistinguishable from a broken button, and the clipboard API is
 * refused often enough — an insecure origin, a denied permission — that the failure
 * needs saying too.
 */
export function CopyButton({ value, label = 'Copy' }: CopyButtonProps) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current) }, [])

  const flash = (next: 'copied' | 'failed') => {
    setState(next)
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => setState('idle'), 1600)
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      flash('copied')
    } catch {
      flash(legacyCopy(value) ? 'copied' : 'failed')
    }
  }

  const text = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label
  const colour =
    state === 'copied' ? 'var(--accent)' : state === 'failed' ? 'var(--status-error)' : 'var(--text-secondary)'

  return (
    <button
      type="button"
      onClick={() => void copy()}
      aria-label={label}
      title={`${label} to clipboard`}
      className="flex h-7 items-center gap-1.5 border px-2 transition-colors hover:bg-[var(--bg-hover)]"
      style={{
        borderRadius: 'var(--radius-sharp)',
        borderColor: state === 'idle' ? 'var(--border-default)' : colour,
        backgroundColor: 'var(--bg-surface)',
        fontSize: 'var(--text-micro)',
        color: colour,
      }}
    >
      {state === 'copied' ? (
        <svg width="11" height="11" viewBox="0 0 12 12" aria-hidden="true">
          <path d="M2 6.5 L4.5 9 L10 3" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ) : (
        <svg width="11" height="11" viewBox="0 0 12 12" aria-hidden="true">
          <rect x="4" y="4" width="6.5" height="6.5" rx="1" fill="none" stroke="currentColor" strokeWidth="1.3" />
          <path d="M8 1.5 H2.5 A1 1 0 0 0 1.5 2.5 V8" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
      )}
      <span>{text}</span>
    </button>
  )
}

/** Older and locked-down browsers refuse the async clipboard but allow this. */
function legacyCopy(value: string): boolean {
  try {
    const area = document.createElement('textarea')
    area.value = value
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    const copied = document.execCommand('copy')
    document.body.removeChild(area)
    return copied
  } catch {
    return false
  }
}
