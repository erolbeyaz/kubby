import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'

import { AuthCard } from '@/components/AuthCard'
import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { Field, TextInput } from '@/components/Field'
import { TotpEnrolment } from '@/components/TotpEnrolment'
import { ApiError, api } from '@/lib/api'

interface LoginScreenProps {
  onAuthenticated: () => void
  /** The last sign-out could not reach the server, so the session may still be live. */
  signOutFailed?: boolean
}

type Stage = 'password' | 'mfa' | 'enrol'

export function LoginScreen({ onAuthenticated, signOutFailed }: LoginScreenProps) {
  const [stage, setStage] = useState<Stage>('password')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [useRecoveryCode, setUseRecoveryCode] = useState(false)
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null)

  const signIn = useMutation({
    mutationFn: () => api.login({ email, password }),
    onSuccess: (result) => {
      if (result.mfaEnrolmentRequired) {
        // Policy demands a second factor and this account has none yet, so the user
        // must enrol before anything else becomes reachable.
        setStage('enrol')
        return
      }
      if (result.mfaRequired) {
        setStage('mfa')
        return
      }
      onAuthenticated()
    },
  })

  const enroll = useMutation({ mutationFn: () => api.enrollMfa() })
  const confirmEnrolment = useMutation({
    mutationFn: () => api.confirmMfa({ code }),
    onSuccess: (result) => setRecoveryCodes(result.recoveryCodes),
  })

  const verify = useMutation({
    mutationFn: () =>
      api.verifyMfa(useRecoveryCode ? { recoveryCode: code } : { code }),
    onSuccess: onAuthenticated,
  })

  const active = stage === 'password' ? signIn : verify
  const error = active.error instanceof ApiError ? active.error : null
  const enrolError =
    confirmEnrolment.error instanceof ApiError
      ? confirmEnrolment.error
      : enroll.error instanceof ApiError
        ? enroll.error
        : null

  if (stage === 'enrol') {
    if (recoveryCodes) {
      return (
        <AuthCard
          title="Save your recovery codes"
          subtitle="Each code works once. They are shown now and never again."
        >
          <div
            className="mb-4 grid grid-cols-2 gap-1 border p-3 font-mono text-[13px]"
            style={{ borderColor: 'var(--border-default)', backgroundColor: 'var(--bg-base)' }}
          >
            {recoveryCodes.map((entry) => (
              <span key={entry} style={{ color: 'var(--text-primary)' }}>
                {entry}
              </span>
            ))}
          </div>
          <Button variant="primary" onClick={onAuthenticated}>
            I have saved them — continue
          </Button>
        </AuthCard>
      )
    }

    return (
      <AuthCard
        title="Set up two-factor authentication"
        subtitle="Your role requires a second factor. Enrol now to finish signing in."
      >
        {!enroll.data ? (
          <div className="flex flex-col gap-3">
            <p className="text-[13px]" style={{ color: 'var(--text-secondary)' }}>
              Kubby holds cluster-wide privileges, so administrators must use an
              authenticator app.
            </p>
            {enrolError && (
              <Callout tone="error" title="Could not start enrolment" requestId={enrolError.requestId}>
                {enrolError.message}
              </Callout>
            )}
            <Button variant="primary" loading={enroll.isPending} onClick={() => enroll.mutate()}>
              Start enrolment
            </Button>
          </div>
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(event) => {
              event.preventDefault()
              confirmEnrolment.mutate()
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

            {enrolError && (
              <Callout tone="error" title="Could not confirm" requestId={enrolError.requestId}>
                {enrolError.message}
              </Callout>
            )}

            <Button type="submit" variant="primary" loading={confirmEnrolment.isPending}>
              Confirm
            </Button>
          </form>
        )}
      </AuthCard>
    )
  }

  if (stage === 'mfa') {
    return (
      <AuthCard
        title="Two-factor authentication"
        subtitle={
          useRecoveryCode
            ? 'Enter one of the recovery codes you saved during enrolment.'
            : 'Enter the six-digit code from your authenticator app.'
        }
      >
        <form
          className="flex flex-col gap-3"
          onSubmit={(event) => {
            event.preventDefault()
            verify.mutate()
          }}
        >
          <Field label={useRecoveryCode ? 'Recovery code' : 'Verification code'}>
            {(id) => (
              <TextInput
                id={id}
                required
                autoFocus
                autoComplete="one-time-code"
                inputMode={useRecoveryCode ? 'text' : 'numeric'}
                placeholder={useRecoveryCode ? 'xxxx-xxxx-xxxx-xxxx' : '000000'}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                style={{ fontFamily: 'var(--font-mono)', letterSpacing: '0.1em' }}
              />
            )}
          </Field>

          {error && (
            <Callout tone="error" title="Verification failed" requestId={error.requestId}>
              {error.message}
            </Callout>
          )}

          <Button type="submit" variant="primary" loading={verify.isPending}>
            Verify
          </Button>

          <button
            type="button"
            className="text-[13px] underline-offset-2 hover:underline"
            style={{ color: 'var(--text-muted)' }}
            onClick={() => {
              setUseRecoveryCode((value) => !value)
              setCode('')
              verify.reset()
            }}
          >
            {useRecoveryCode ? 'Use your authenticator app instead' : 'Use a recovery code instead'}
          </button>
        </form>
      </AuthCard>
    )
  }

  return (
    <AuthCard title="Sign in" subtitle="Kubby manages clusters with full privileges. Sessions are revocable.">
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault()
          signIn.mutate()
        }}
      >
        <Field label="Email">
          {(id) => (
            <TextInput
              id={id}
              type="email"
              required
              autoFocus
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          )}
        </Field>

        <Field label="Password">
          {(id) => (
            <TextInput
              id={id}
              type="password"
              required
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>

        {signOutFailed && (
          <Callout tone="warning" title="Sign-out may not have completed">
            The server did not confirm your sign-out, so the session could still be
            active. Sign in again and use "Active sessions" to revoke it.
          </Callout>
        )}

        {error && <SignInFailure error={error} />}

        <Button type="submit" variant="primary" loading={signIn.isPending}>
          Sign in
        </Button>
      </form>
    </AuthCard>
  )
}


/** Explains a failed sign-in: attempts left, how long a lockout runs, or a block. */
function SignInFailure({ error }: { error: ApiError }) {
  const { attemptsRemaining, lockedForSeconds, blocked } = error.detail

  if (blocked) {
    return (
      <Callout tone="error" title="Account blocked" requestId={error.requestId}>
        This account was blocked after repeated failed sign-ins. Ask an administrator to
        unblock it.
      </Callout>
    )
  }

  if (lockedForSeconds && lockedForSeconds > 0) {
    return (
      <Callout tone="warning" title="Account temporarily locked" requestId={error.requestId}>
        Too many failed attempts. Try again in {formatLockout(lockedForSeconds)}.
      </Callout>
    )
  }

  return (
    <Callout tone="error" title="Sign-in failed" requestId={error.requestId}>
      {error.message}
      {attemptsRemaining !== undefined && attemptsRemaining > 0 && (
        <span style={{ display: 'block', marginTop: '0.35rem', color: 'var(--status-warn)' }}>
          {attemptsRemaining} {attemptsRemaining === 1 ? 'attempt' : 'attempts'} remaining
          before this account is locked.
        </span>
      )}
    </Callout>
  )
}

function formatLockout(seconds: number): string {
  const minutes = Math.ceil(seconds / 60)
  if (minutes <= 1) return 'about a minute'
  return `about ${minutes} minutes`
}
