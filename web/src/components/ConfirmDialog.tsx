import { useEffect, useRef, useState, type ReactNode } from 'react'

import { TextInput } from '@/components/Field'

interface ConfirmDialogProps {
  title: string
  /** What is about to happen, in the words of the thing it happens to. */
  children: ReactNode
  /**
   * Typed exactly before the action unlocks. Omit it for a plain yes-or-no gate.
   */
  phrase?: string
  confirmLabel: string
  destructive?: boolean
  busy?: boolean
  /** Held back for a reason other than being in flight — an answer that is not valid yet. */
  disabled?: boolean
  error?: string | undefined
  onConfirm: () => void
  onCancel: () => void
}

/**
 * The gate in front of a destructive action.
 *
 * A single object is confirmed by typing its name: it is the only thing that makes the
 * reader look at which one they picked. A gate over several objects lists them instead —
 * the list is what has to be read, and a phrase in front of it is a second lock on a
 * door the reader already opened deliberately.
 */
export function ConfirmDialog({
  title,
  children,
  phrase,
  confirmLabel,
  destructive = false,
  busy = false,
  disabled = false,
  error,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const [typed, setTyped] = useState('')
  const field = useRef<HTMLInputElement>(null)

  useEffect(() => {
    field.current?.focus()

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onCancel])

  const matches = !phrase || typed.trim() === phrase

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center p-4"
      style={{ backgroundColor: 'rgb(0 0 0 / 0.55)' }}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onCancel()
      }}
    >
      {/* Three bands: what this is about, what you are choosing, and what you do about
          it. The rules between them are what stop a dialog reading as one paragraph
          with buttons at the end. */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="w-full max-w-lg overflow-hidden border shadow-2xl"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-strong)',
          borderRadius: '0.6rem',
        }}
      >
        <header
          className="border-b px-5 py-3 text-center"
          style={{ borderColor: 'var(--border-default)' }}
        >
          <h2
            style={{
              fontSize: 'var(--text-body)',
              color: destructive ? 'var(--status-error)' : 'var(--text-primary)',
            }}
          >
            {title}
          </h2>
        </header>

        <div
          className="px-5 py-4"
          style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}
        >
          {children}

          {phrase && (
            <label className="mt-4 block">
              <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                Type <code style={{ color: 'var(--text-primary)' }}>{phrase}</code> to confirm
              </span>
              <span className="mt-1 block">
                <TextInput
                  ref={field}
                  value={typed}
                  aria-label="Confirmation"
                  onChange={(event) => setTyped(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' && matches && !busy && !disabled) onConfirm()
                  }}
                  style={{ width: '100%' }}
                />
              </span>
            </label>
          )}

          {error && (
            <p className="mt-3" style={{ color: 'var(--status-error)' }}>
              {error}
            </p>
          )}
        </div>

        <footer
          className="flex items-center justify-between border-t px-5 py-3"
          style={{ borderColor: 'var(--border-default)' }}
        >
          <button
            type="button"
            onClick={onCancel}
            className="h-8 border px-3.5 transition-colors hover:bg-[var(--bg-hover)]"
            style={{
              borderRadius: 'var(--radius-sharp)',
              borderColor: 'var(--border-default)',
              fontSize: 'var(--text-secondary-size)',
              color: 'var(--text-secondary)',
            }}
          >
            Cancel
          </button>

          <button
            type="button"
            disabled={!matches || busy || disabled}
            onClick={onConfirm}
            className="h-8 px-3.5 font-medium transition-opacity disabled:cursor-not-allowed disabled:opacity-40"
            style={{
              borderRadius: 'var(--radius-sharp)',
              backgroundColor: destructive ? 'var(--status-error)' : 'var(--accent)',
              fontSize: 'var(--text-secondary-size)',
              color: 'var(--text-inverse)',
            }}
          >
            {busy ? 'Working…' : confirmLabel}
          </button>
        </footer>
      </div>
    </div>
  )
}
