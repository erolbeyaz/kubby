import type { InputHTMLAttributes, ReactNode, Ref, SelectHTMLAttributes } from 'react'
import { useId } from 'react'

const controlStyle = {
  backgroundColor: 'var(--bg-base)',
  borderColor: 'var(--border-default)',
  borderRadius: 'var(--radius-sharp)',
  color: 'var(--text-primary)',
} as const

interface FieldProps {
  label: string
  hint?: ReactNode
  error?: string | undefined
  children: (id: string) => ReactNode
}

/** Labels every control and keeps the error message tied to it for screen readers. */
export function Field({ label, hint, error, children }: FieldProps) {
  const id = useId()

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-[13px] font-medium" style={{ color: 'var(--text-secondary)' }}>
        {label}
      </label>
      {children(id)}
      {error && (
        <p id={`${id}-error`} role="alert" className="text-[13px]" style={{ color: 'var(--status-error)' }}>
          {error}
        </p>
      )}
      {hint && !error && (
        <p className="text-[13px]" style={{ color: 'var(--text-muted)' }}>
          {hint}
        </p>
      )}
    </div>
  )
}

type TextInputProps = InputHTMLAttributes<HTMLInputElement> & {
  invalid?: boolean
  ref?: Ref<HTMLInputElement>
}

export function TextInput({ invalid, style, ref, ...props }: TextInputProps) {
  return (
    <input
      {...props}
      ref={ref}
      aria-invalid={invalid ?? undefined}
      className="h-9 border px-2.5 text-[13px] outline-none transition-colors focus:border-[var(--accent)]"
      style={{
        ...controlStyle,
        ...(invalid ? { borderColor: 'var(--status-error)' } : {}),
        ...style,
      }}
    />
  )
}

export function Select({ style, children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className="h-9 border px-2.5 text-[13px] outline-none focus:border-[var(--accent)]"
      style={{ ...controlStyle, ...style }}
    >
      {children}
    </select>
  )
}
