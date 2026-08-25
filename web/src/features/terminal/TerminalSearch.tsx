import { useEffect, useRef, useState } from 'react'

export interface SearchState {
  current: number
  total: number
}

interface TerminalSearchProps {
  matches: SearchState
  onSearch: (query: string, options: { caseSensitive: boolean; regex: boolean }) => void
  onStep: (
    direction: 'next' | 'previous',
    query: string,
    options: { caseSensitive: boolean; regex: boolean },
  ) => void
}

/**
 * Finding something in what a session has already printed.
 *
 * A terminal's scrollback is the only record of what happened before the reader looked,
 * and it is exactly the moment they need to find "the line where it failed". Scrolling
 * ten thousand rows by eye is not that.
 */
export function TerminalSearch({ matches, onSearch, onStep }: TerminalSearchProps) {
  const [query, setQuery] = useState('')
  const [caseSensitive, setCaseSensitive] = useState(false)
  const [regex, setRegex] = useState(false)
  const field = useRef<HTMLInputElement>(null)

  useEffect(() => {
    onSearch(query, { caseSensitive, regex })
  }, [query, caseSensitive, regex, onSearch])

  // Ctrl+F is what everyone's hands already do in a terminal window.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key === 'f') {
        event.preventDefault()
        field.current?.focus()
        field.current?.select()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])

  const options = { caseSensitive, regex }

  return (
    <div
      className="flex h-9 shrink-0 items-center gap-1.5 border-b px-2"
      style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
    >
      <Toggle label="Aa" title="Match case" on={caseSensitive} onClick={() => setCaseSensitive((v) => !v)} />
      <Toggle label=".*" title="Regular expression" on={regex} onClick={() => setRegex((v) => !v)} />

      <input
        ref={field}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== 'Enter') return
          event.preventDefault()
          onStep(event.shiftKey ? 'previous' : 'next', query, options)
        }}
        placeholder="Search in terminal…"
        aria-label="Search in terminal"
        className="h-6 min-w-0 flex-1 border px-2"
        style={{
          borderRadius: 'var(--radius-sharp)',
          borderColor: 'var(--border-default)',
          backgroundColor: 'var(--bg-raised)',
          color: 'var(--text-primary)',
          fontSize: 'var(--text-micro)',
        }}
      />

      <span
        className="shrink-0 font-mono tabular-nums"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        {matches.total === 0 ? '0/0' : `${matches.current}/${matches.total}`}
      </span>

      <Step
        title="Previous match"
        path="M3 7.5 L6.5 4 L10 7.5"
        disabled={matches.total === 0}
        onClick={() => onStep('previous', query, options)}
      />
      <Step
        title="Next match"
        path="M3 4.5 L6.5 8 L10 4.5"
        disabled={matches.total === 0}
        onClick={() => onStep('next', query, options)}
      />
    </div>
  )
}

function Toggle({
  label,
  title,
  on,
  onClick,
}: {
  label: string
  title: string
  on: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-pressed={on}
      className="flex h-6 w-6 shrink-0 items-center justify-center font-mono transition-colors hover:bg-[var(--bg-hover)]"
      style={{
        borderRadius: 'var(--radius-sharp)',
        fontSize: 'var(--text-micro)',
        color: on ? 'var(--accent)' : 'var(--text-muted)',
        backgroundColor: on ? 'var(--accent-muted)' : undefined,
      }}
    >
      {label}
    </button>
  )
}

function Step({
  title,
  path,
  disabled,
  onClick,
}: {
  title: string
  path: string
  disabled: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      disabled={disabled}
      onClick={onClick}
      className="flex h-6 w-6 shrink-0 items-center justify-center transition-colors hover:bg-[var(--bg-hover)] disabled:opacity-40"
      style={{ borderRadius: 'var(--radius-sharp)', color: 'var(--text-muted)' }}
    >
      <svg width="13" height="13" viewBox="0 0 13 13" aria-hidden="true">
        <path d={path} fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </button>
  )
}
