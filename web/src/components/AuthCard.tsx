import type { ReactNode } from 'react'

import { Logo } from '@/components/Logo'

interface AuthCardProps {
  title: string
  subtitle?: string
  children: ReactNode
  footer?: ReactNode
}

/** The centred panel used by the setup wizard and the sign-in screens. */
export function AuthCard({ title, subtitle, children, footer }: AuthCardProps) {
  return (
    <div className="flex h-full items-center justify-center p-6" style={{ backgroundColor: 'var(--bg-base)' }}>
      <div
        className="w-full max-w-md border p-7"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-default)',
          borderRadius: 'var(--radius-panel)',
        }}
      >
        <div className="mb-6 flex flex-col items-center text-center">
          <Logo size={56} showWordmark />
          <h1
            className="mt-5 font-semibold"
            style={{ fontSize: 'var(--text-title)', color: 'var(--text-primary)' }}
          >
            {title}
          </h1>
          {subtitle && (
            <p
              className="mt-1.5"
              style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-muted)' }}
            >
              {subtitle}
            </p>
          )}
        </div>

        {children}

        {footer && <div className="mt-5 border-t pt-4" style={{ borderColor: 'var(--border-subtle)' }}>{footer}</div>}
      </div>
    </div>
  )
}
