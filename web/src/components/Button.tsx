import type { ButtonHTMLAttributes } from 'react'

type Variant = 'primary' | 'secondary' | 'danger'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  loading?: boolean
}

const VARIANTS: Record<Variant, { bg: string; fg: string; border: string }> = {
  primary: { bg: 'var(--accent)', fg: 'var(--text-inverse)', border: 'var(--accent)' },
  secondary: { bg: 'transparent', fg: 'var(--text-secondary)', border: 'var(--border-default)' },
  danger: { bg: 'transparent', fg: 'var(--status-error)', border: 'var(--status-error)' },
}

export function Button({ variant = 'secondary', loading, disabled, children, ...props }: ButtonProps) {
  const colors = VARIANTS[variant]

  return (
    <button
      {...props}
      disabled={disabled ?? loading}
      className="inline-flex h-9 items-center justify-center gap-1.5 border px-3.5 text-[13px] font-medium transition-opacity disabled:cursor-not-allowed disabled:opacity-50"
      style={{
        backgroundColor: colors.bg,
        color: colors.fg,
        borderColor: colors.border,
        borderRadius: 'var(--radius-sharp)',
      }}
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
