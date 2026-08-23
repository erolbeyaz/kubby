import type { ReactNode } from 'react'

import { Callout } from '@/components/Callout'

interface SettingsSectionProps {
  title: string
  description: string
  children: ReactNode
  busy: boolean
  saved: boolean
  error: string
  onSave: () => void
}

/** The shell every settings group shares: a heading, the fields, and one save. */
export function SettingsSection({
  title,
  description,
  children,
  busy,
  saved,
  error,
  onSave,
}: SettingsSectionProps) {
  return (
    <form
      className="max-w-2xl p-5"
      onSubmit={(event) => {
        event.preventDefault()
        onSave()
      }}
    >
      <h2 className="font-semibold" style={{ fontSize: 'var(--text-title)', color: 'var(--text-primary)' }}>
        {title}
      </h2>
      <p className="mt-1" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-muted)' }}>
        {description}
      </p>

      <div className="mt-5 flex flex-col gap-4">{children}</div>

      {error && (
        <div className="mt-4">
          <Callout tone="error" title="Could not save">
            {error}
          </Callout>
        </div>
      )}

      <div className="mt-5 flex items-center gap-3">
        <button
          type="submit"
          disabled={busy}
          className="h-9 px-4 font-medium transition-opacity disabled:opacity-50"
          style={{
            borderRadius: 'var(--radius-sharp)',
            backgroundColor: 'var(--accent)',
            fontSize: 'var(--text-secondary-size)',
            color: 'var(--text-inverse)',
          }}
        >
          {busy ? 'Saving…' : 'Save'}
        </button>

        {saved && (
          <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--accent)' }}>Saved.</span>
        )}
      </div>
    </form>
  )
}
