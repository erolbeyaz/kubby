import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'

import { AuthCard } from '@/components/AuthCard'
import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { Field, TextInput } from '@/components/Field'
import { ApiError, api } from '@/lib/api'

interface SetupWizardProps {
  onComplete: () => void
}

/**
 * First-run wizard. This is the only path that creates an account without an existing
 * administrator, and the server refuses it once any user exists.
 */
export function SetupWizard({ onComplete }: SetupWizardProps) {
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')

  const mismatch = confirmation.length > 0 && password !== confirmation

  const create = useMutation({
    mutationFn: () => api.createFirstAdmin({ email, displayName, password }),
    onSuccess: onComplete,
  })

  const error = create.error instanceof ApiError ? create.error : null

  return (
    <AuthCard
      title="Create the first administrator"
      subtitle="Kubby has no accounts yet. This wizard runs once and closes permanently."
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault()
          if (!mismatch) create.mutate()
        }}
      >
        <Field label="Email">
          {(id) => (
            <TextInput
              id={id}
              type="email"
              required
              autoComplete="username"
              autoFocus
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          )}
        </Field>

        <Field label="Display name">
          {(id) => (
            <TextInput
              id={id}
              required
              autoComplete="name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          )}
        </Field>

        <Field
          label="Password"
          hint="At least 12 characters, combining three of: uppercase, lowercase, digits, symbols."
        >
          {(id) => (
            <TextInput
              id={id}
              type="password"
              required
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>

        <Field label="Confirm password" error={mismatch ? 'Passwords do not match' : undefined}>
          {(id) => (
            <TextInput
              id={id}
              type="password"
              required
              autoComplete="new-password"
              invalid={mismatch}
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value)}
            />
          )}
        </Field>

        {error && (
          <Callout tone="error" title="Could not create the account" requestId={error.requestId}>
            {error.message}
          </Callout>
        )}

        <Button type="submit" variant="primary" loading={create.isPending}>
          Create administrator
        </Button>
      </form>
    </AuthCard>
  )
}
