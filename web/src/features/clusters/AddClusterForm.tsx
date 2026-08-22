import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { Field, Select, TextInput } from '@/components/Field'
import { ApiError, api, type Environment } from '@/lib/api'

import { ENVIRONMENTS, ENVIRONMENT_HINT, ENVIRONMENT_COLOR } from './environment'
import { KubeconfigPaste } from './KubeconfigPaste'

interface AddClusterFormProps {
  onCreated: () => void
}

export function AddClusterForm({ onCreated }: AddClusterFormProps) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [environment, setEnvironment] = useState<Environment>('test')
  const [environmentLabel, setEnvironmentLabel] = useState('')

  const create = useMutation({
    mutationFn: (input: { kubeconfig: string; contextName: string }) =>
      api.createCluster({
        name,
        environment,
        environmentLabel,
        color: '',
        kubeconfig: input.kubeconfig,
        contextName: input.contextName,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['clusters'] })
      onCreated()
    },
  })

  const error = create.error instanceof ApiError ? create.error : null

  return (
    <div className="flex flex-col gap-5">
      <div className="grid gap-4 sm:grid-cols-3">
        <Field label="Cluster name">
          {(id) => (
            <TextInput
              id={id}
              required
              autoFocus
              placeholder="prod-app"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          )}
        </Field>

        <Field label="Environment" hint={ENVIRONMENT_HINT[environment]}>
          {(id) => (
            <Select
              id={id}
              value={environment}
              onChange={(e) => setEnvironment(e.target.value as Environment)}
              style={{ borderLeft: `3px solid ${ENVIRONMENT_COLOR[environment]}` }}
            >
              {ENVIRONMENTS.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </Select>
          )}
        </Field>

        <Field label="Label (optional)" hint="What your team calls it. Shown instead of the code.">
          {(id) => (
            <TextInput
              id={id}
              placeholder="Production"
              value={environmentLabel}
              onChange={(e) => setEnvironmentLabel(e.target.value)}
            />
          )}
        </Field>
      </div>

      {error && (
        <Callout tone="error" title="Could not add the cluster" requestId={error.requestId}>
          {error.message}
        </Callout>
      )}

      <KubeconfigPaste
        confirmLabel="Add cluster"
        busy={create.isPending}
        onConfirm={(kubeconfig, contextName) => {
          if (name.trim().length > 0) create.mutate({ kubeconfig, contextName })
        }}
      />

      {name.trim().length === 0 && (
        <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          Give the cluster a name before adding it.
        </p>
      )}
    </div>
  )
}
