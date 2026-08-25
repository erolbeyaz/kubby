export interface SearchOptions {
  term: string
  matchCase: boolean
  regex: boolean
  onlyMatching: boolean
}

interface LogSearchProps {
  value: SearchOptions
  onChange: (next: SearchOptions) => void
}

/**
 * The search field, with its own controls inside it.
 *
 * Case sensitivity and regular expressions belong to the search, not to the toolbar
 * around it: a log search is often for a request id or a stack frame, where the wrong
 * case or an unescaped dot is the difference between one match and two thousand.
 */
export function LogSearch({ value, onChange }: LogSearchProps) {
  const set = (patch: Partial<SearchOptions>) => onChange({ ...value, ...patch })

  return (
    <div
      className="flex h-7 w-full items-center gap-1 border px-1"
      style={{
        borderRadius: 'var(--radius-sharp)',
        // Filtering hides part of the log, so the field itself says so rather than
        // leaving the reader to wonder where the other lines went.
        borderColor: value.onlyMatching && value.term ? 'var(--accent)' : 'var(--border-default)',
        backgroundColor: 'var(--bg-base)',
      }}
    >
      {/* The funnel filters; the two beside it change what counts as a match. They were
          easy to mistake for each other, so each says plainly what it does. */}
      <InlineToggle
        label="Filter: hide lines that do not match"
        active={value.onlyMatching}
        onClick={() => set({ onlyMatching: !value.onlyMatching })}
      >
        <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
          <path d="M2.5 3h11l-4.2 5v4.5l-2.6 1.5V8z" strokeLinejoin="round" />
        </svg>
      </InlineToggle>

      <InlineToggle
        label="Match case: treat upper and lower case as different"
        active={value.matchCase}
        onClick={() => set({ matchCase: !value.matchCase })}
      >
        <span style={{ fontSize: 11, fontWeight: 600, lineHeight: 1 }}>Aa</span>
      </InlineToggle>

      <InlineToggle
        label="Regular expression: read the search text as a pattern"
        active={value.regex}
        onClick={() => set({ regex: !value.regex })}
      >
        <span className="font-mono" style={{ fontSize: 11, fontWeight: 600, lineHeight: 1 }}>
          .*
        </span>
      </InlineToggle>

      <input
        value={value.term}
        onChange={(event) => set({ term: event.target.value })}
        placeholder={value.onlyMatching ? 'Filter logs' : 'Search in logs'}
        aria-label="Search in logs"
        className="min-w-0 flex-1 bg-transparent px-1 outline-none"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-primary)' }}
      />

      {value.term && (
        <InlineToggle label="Clear search" active={false} onClick={() => set({ term: '' })}>
          <svg width="11" height="11" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
            <path d="M2 2 L10 10 M10 2 L2 10" strokeLinecap="round" />
          </svg>
        </InlineToggle>
      )}
    </div>
  )
}

function InlineToggle({
  label,
  active,
  onClick,
  children,
}: {
  label: string
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      aria-pressed={active}
      className={`tool-chip${active ? ' tool-chip--active' : ''}`}
    >
      {children}
    </button>
  )
}

/**
 * Finds the lines a search matches.
 *
 * An invalid regular expression is a half-typed one, not an error: reporting it while
 * someone is still typing "\\d{" would make the field shout at every keystroke.
 */
export function matchesFor<T extends { id: number; text: string }>(
  lines: T[],
  search: SearchOptions,
): number[] {
  if (!search.term) return []

  if (search.regex) {
    let pattern: RegExp
    try {
      pattern = new RegExp(search.term, search.matchCase ? '' : 'i')
    } catch {
      return []
    }
    return lines.filter((line) => pattern.test(line.text)).map((line) => line.id)
  }

  if (search.matchCase) {
    return lines.filter((line) => line.text.includes(search.term)).map((line) => line.id)
  }
  const needle = search.term.toLowerCase()
  return lines.filter((line) => line.text.toLowerCase().includes(needle)).map((line) => line.id)
}
