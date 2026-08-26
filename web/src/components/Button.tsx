import type { ButtonHTMLAttributes, CSSProperties } from 'react'

type Variant = 'primary' | 'secondary' | 'danger'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  loading?: boolean
}

/**
 * Colours arrive as custom properties, not as `background-color`.
 *
 * An inline style always beats a stylesheet rule, so a `:hover` written in CSS against an
 * inline background silently never fires — which is why these buttons did nothing under
 * the pointer. Handing the variant in as variables lets `.btn:hover` in theme.css do the
 * work, and keeps every colour a token rather than a literal.
 */
const VARIANTS: Record<Variant, CSSProperties> = {
  primary: {
    '--btn-bg': 'var(--accent)',
    '--btn-fg': 'var(--text-inverse)',
    '--btn-border': 'var(--accent)',
    '--btn-bg-hover': 'var(--accent-hover)',
    '--btn-border-hover': 'var(--accent-hover)',
    '--btn-fg-hover': 'var(--text-inverse)',
  } as CSSProperties,
  secondary: {
    '--btn-bg': 'transparent',
    '--btn-fg': 'var(--text-secondary)',
    '--btn-border': 'var(--border-default)',
    '--btn-bg-hover': 'var(--bg-hover)',
    '--btn-border-hover': 'var(--border-strong)',
    '--btn-fg-hover': 'var(--text-primary)',
  } as CSSProperties,
  danger: {
    '--btn-bg': 'transparent',
    '--btn-fg': 'var(--status-error)',
    '--btn-border': 'var(--status-error)',
    // Tinted rather than filled: a destructive button that turns solid red under the
    // pointer reads as already pressed.
    '--btn-bg-hover': 'color-mix(in srgb, var(--status-error) 16%, transparent)',
    '--btn-border-hover': 'var(--status-error)',
    '--btn-fg-hover': 'var(--status-error)',
  } as CSSProperties,
}

export function Button({ variant = 'secondary', loading, disabled, children, ...props }: ButtonProps) {
  return (
    <button
      {...props}
      disabled={disabled ?? loading}
      className="btn inline-flex h-9 items-center justify-center gap-1.5 border px-3.5 text-[13px] font-medium disabled:cursor-not-allowed disabled:opacity-50"
      style={{ ...VARIANTS[variant], borderRadius: 'var(--radius-sharp)' }}
    >
      {loading && (
        <span
          aria-hidden="true"
          className="inline-block h-2.5 w-2.5 animate-spin border border-current border-t-transparent"
          style={{ borderRadius: '50%' }}
        />
      )}
      {children}
    </button>
  )
}
