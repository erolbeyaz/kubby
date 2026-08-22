import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { Field, TextInput } from '@/components/Field'
import { TotpEnrolment } from '@/components/TotpEnrolment'
import { ApiError, api, type Me } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

interface AccountScreenProps {
  me: Me
  focus?: 'password' | 'mfa' | 'sessions' | null
}

export function AccountScreen({ me, focus }: AccountScreenProps) {
  const password = useRef<HTMLDivElement>(null)
  const mfa = useRef<HTMLDivElement>(null)
  const sessions = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const target = focus === 'password' ? password : focus === 'mfa' ? mfa : focus === 'sessions' ? sessions : null
    target?.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [focus])

  return (
    <div className="flex h-full flex-col gap-6 overflow-y-auto p-6">
      <div ref={password}>
        <PasswordSection />
      </div>
      <div ref={mfa}>
        <MfaSection enrolled={me.mfaEnrolled} />
      </div>
      <div ref={sessions}>
        <SessionsSection />
      </div>
    </div>
  )
}

function Section({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <section
      className="border"
      style={{
        borderColor: 'var(--border-subtle)',
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-panel)',
      }}
    >
      <header className="border-b px-4 py-2.5" style={{ borderColor: 'var(--border-subtle)' }}>
        <h2 className="text-[13px] font-semibold" style={{ color: 'var(--text-primary)' }}>
          {title}
        </h2>
        <p className="mt-0.5 text-[13px]" style={{ color: 'var(--text-muted)' }}>
          {description}
        </p>
      </header>
      <div className="p-4">{children}</div>
    </section>
  )
}

function PasswordSection() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')

  const change = useMutation({
    mutationFn: () => api.changePassword({ currentPassword, newPassword }),
    onSuccess: () => {
      setCurrentPassword('')
      setNewPassword('')
    },
  })

  const error = change.error instanceof ApiError ? change.error : null

  return (
    <Section
      title="Password"
      description="Changing your password signs out every other session."
    >
      <form
        className="flex max-w-sm flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault()
          change.mutate()
        }}
      >
        <Field label="Current password">
          {(id) => (
            <TextInput
              id={id}
              type="password"
              required
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
            />
          )}
        </Field>
        <Field label="New password">
          {(id) => (
            <TextInput
              id={id}
              type="password"
              required
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
            />
          )}
        </Field>

        {error && (
          <Callout tone="error" title="Could not change the password" requestId={error.requestId}>
            {error.message}
          </Callout>
        )}
        {change.isSuccess && (
          <Callout tone="success">Password changed. Other sessions were signed out.</Callout>
        )}

        <Button type="submit" variant="primary" loading={change.isPending}>
          Change password
        </Button>
      </form>
    </Section>
  )
}

function MfaSection({ enrolled }: { enrolled: boolean }) {
  const queryClient = useQueryClient()
  const [code, setCode] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null)

  const enroll = useMutation({ mutationFn: () => api.enrollMfa() })
  const confirm = useMutation({
    mutationFn: () => api.confirmMfa({ code }),
    onSuccess: (result) => {
      setRecoveryCodes(result.recoveryCodes)
      setCode('')
      void queryClient.invalidateQueries({ queryKey: ['me'] })
    },
  })

  const error =
    confirm.error instanceof ApiError ? confirm.error : enroll.error instanceof ApiError ? enroll.error : null

  if (recoveryCodes) {
    return (
      <Section
        title="Recovery codes"
        description="Save these now. They are shown once and each one works a single time."
      >
        <div
          className="mb-3 grid grid-cols-2 gap-1 border p-3 font-mono text-[13px]"
          style={{ borderColor: 'var(--border-default)', backgroundColor: 'var(--bg-base)' }}
        >
          {recoveryCodes.map((entry) => (
            <span key={entry} style={{ color: 'var(--text-primary)' }}>
              {entry}
            </span>
          ))}
        </div>
        <Button onClick={() => setRecoveryCodes(null)}>I have saved them</Button>
      </Section>
    )
  }

  if (enrolled) {
    return (
      <Section title="Two-factor authentication" description="Your account is protected by an authenticator app.">
        <Callout tone="success">Two-factor authentication is active.</Callout>
      </Section>
    )
  }

  return (
    <Section
      title="Two-factor authentication"
      description="Kubby holds cluster-wide privileges; a password alone is a thin defence."
    >
      {!enroll.data ? (
        <Button variant="primary" loading={enroll.isPending} onClick={() => enroll.mutate()}>
          Start enrolment
        </Button>
      ) : (
        <form
          className="flex max-w-sm flex-col gap-3"
          onSubmit={(event) => {
            event.preventDefault()
            confirm.mutate()
          }}
        >
          <TotpEnrolment secret={enroll.data.secret} qrCodePng={enroll.data.qrCodePng} />

          <Field label="Verification code">
            {(id) => (
              <TextInput
                id={id}
                required
                autoFocus
                inputMode="numeric"
                placeholder="000000"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                style={{ fontFamily: 'var(--font-mono)', letterSpacing: '0.1em' }}
              />
            )}
          </Field>

          {error && (
            <Callout tone="error" title="Could not confirm" requestId={error.requestId}>
              {error.message}
            </Callout>
          )}

          <Button type="submit" variant="primary" loading={confirm.isPending}>
            Confirm and generate recovery codes
          </Button>
        </form>
      )}
    </Section>
  )
}

function SessionsSection() {
  const queryClient = useQueryClient()

  const sessions = useQuery({
    queryKey: ['sessions'],
    queryFn: ({ signal }) => api.sessions(signal),
  })

  const revoke = useMutation({
    mutationFn: () => api.revokeOtherSessions(),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['sessions'] }),
  })

  const list = sessions.data?.sessions ?? []
  const others = list.filter((s) => !s.current).length

  return (
    <Section title="Active sessions" description="Every session can be revoked from the server.">
      {sessions.isLoading && (
        <p className="text-[13px]" style={{ color: 'var(--text-muted)' }}>
          Loading sessions…
        </p>
      )}

      {list.length > 0 && (
        <div className="mb-3 overflow-x-auto">
          <table className="w-full text-left text-[13px]">
            <thead>
              <tr style={{ color: 'var(--text-muted)' }}>
                <th className="py-1 pr-4 font-medium">Client</th>
                <th className="py-1 pr-4 font-medium">Address</th>
                <th className="py-1 pr-4 font-medium">Last seen</th>
                <th className="py-1 font-medium">Expires</th>
              </tr>
            </thead>
            <tbody className="font-mono" style={{ color: 'var(--text-secondary)' }}>
              {list.map((session) => (
                <tr key={session.id} className="border-t" style={{ borderColor: 'var(--border-subtle)' }}>
                  <td className="max-w-56 truncate py-1.5 pr-4" title={session.userAgent}>
                    {session.userAgent || '—'}
                    {session.current && (
                      <span className="ml-1.5 not-italic" style={{ color: 'var(--accent)' }}>
                        (this device)
                      </span>
                    )}
                  </td>
                  <td className="py-1.5 pr-4">{session.ipAddress ?? '—'}</td>
                  <td className="py-1.5 pr-4" title={formatAbsolute(session.lastSeenAt)}>
                    {formatAge(session.lastSeenAt)} ago
                  </td>
                  <td className="py-1.5" title={formatAbsolute(session.expiresAt)}>
                    in {formatAge(new Date(), new Date(session.expiresAt))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Button variant="danger" disabled={others === 0} loading={revoke.isPending} onClick={() => revoke.mutate()}>
        {others === 0 ? 'No other sessions' : `Sign out ${others} other session${others === 1 ? '' : 's'}`}
      </Button>
    </Section>
  )
}
