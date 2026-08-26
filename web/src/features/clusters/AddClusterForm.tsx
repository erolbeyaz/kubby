import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { Field, TextInput } from '@/components/Field'
import { ApiError, api, type Environment } from '@/lib/api'

import { KubeconfigPaste } from './KubeconfigPaste'
import { ENVIRONMENTS, ENVIRONMENT_COLOR, ENVIRONMENT_HINT, ENVIRONMENT_TITLE } from './environment'

interface AddClusterFormProps {
  onDone: () => void
}

/**
 * Connecting a cluster, as the two steps it actually is.
 *
 * The order is not a presentation choice: the name decides what the cluster is called
 * once it connects, and the server stores nothing until the pasted credential has been
 * proved to work (ADR-018). Numbering says that out loud, so a half-finished screen reads
 * as "step two of two" rather than as a form nobody has filled in.
 *
 * The paste step carries its own sequence — check, choose a context, confirm — and it is
 * left to run it. A number around each of those would be numbering for its own sake: the
 * buttons already say which comes next.
 *
 * It is a page rather than a panel wedged above the list. Adding a cluster is the one act
 * on this screen that deserves the whole window.
 */
export function AddClusterForm({ onDone }: AddClusterFormProps) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [environment, setEnvironment] = useState<Environment>('test')
  const [environmentLabel, setEnvironmentLabel] = useState('')

  const create = useMutation({
    mutationFn: (input: { kubeconfig: string; contextName: string }) =>
      api.createCluster({
        name: name.trim(),
        environment,
        environmentLabel: environmentLabel.trim(),
        color: '',
        kubeconfig: input.kubeconfig,
        contextName: input.contextName,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['clusters'] })
      onDone()
    },
  })

  const error = create.error instanceof ApiError ? create.error : null
  const named = name.trim().length > 0

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex h-12 shrink-0 items-center gap-3 border-b px-4"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <Button onClick={onDone}>← Clusters</Button>
        <h2 className="font-semibold" style={{ fontSize: 'var(--text-title)', color: 'var(--text-primary)' }}>
          Connect a cluster
        </h2>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-3xl flex-col gap-8 px-4 py-6">
          <Step
            number={1}
            title="Name it"
            hint="How your team will refer to this cluster, and which tier it belongs to."
          >
            <div className="grid gap-4 sm:grid-cols-2">
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

              <Field label="Label" hint="Optional. What your team calls this tier.">
                {(id) => (
                  <TextInput
                    id={id}
                    placeholder={ENVIRONMENT_TITLE[environment]}
                    value={environmentLabel}
                    onChange={(e) => setEnvironmentLabel(e.target.value)}
                  />
                )}
              </Field>
            </div>

            {/* Chosen by clicking the tier itself rather than from a dropdown: the tier
                decides what Kubby will let you do here later (ADR-024), and the colour
                you pick now is the colour you will see across the top of every screen. */}
            <fieldset className="mt-4">
              <legend
                className="mb-1.5 font-medium"
                style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}
              >
                Environment
              </legend>
              <div className="flex flex-wrap gap-2">
                {ENVIRONMENTS.map((value) => {
                  const chosen = value === environment
                  return (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={chosen}
                      onClick={() => setEnvironment(value)}
                      className="flex items-center gap-2 border px-3 py-1.5 transition-colors"
                      style={{
                        borderRadius: 'var(--radius-sharp)',
                        borderColor: chosen ? ENVIRONMENT_COLOR[value] : 'var(--border-default)',
                        backgroundColor: chosen
                          ? `color-mix(in srgb, ${ENVIRONMENT_COLOR[value]} 14%, transparent)`
                          : 'transparent',
                        color: chosen ? 'var(--text-primary)' : 'var(--text-secondary)',
                        fontSize: 'var(--text-secondary-size)',
                      }}
                    >
                      <span
                        aria-hidden="true"
                        className="h-2.5 w-2.5 shrink-0"
                        style={{ backgroundColor: ENVIRONMENT_COLOR[value], borderRadius: 2 }}
                      />
                      {ENVIRONMENT_TITLE[value]}
                    </button>
                  )
                })}
              </div>
              <p className="mt-1.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                {ENVIRONMENT_HINT[environment]}
              </p>
            </fieldset>
          </Step>

          <Step
            last
            number={2}
            title="Paste its kubeconfig"
            hint="Checked before anything is stored, and encrypted once it is. It is never shown again."
            muted={!named}
          >
            {named ? (
              <>
                {error && (
                  <Callout tone="error" title="Could not add the cluster" requestId={error.requestId}>
                    {error.message}
                  </Callout>
                )}

                <KubeconfigPaste
                  confirmLabel={`Add ${name.trim()}`}
                  busy={create.isPending}
                  onConfirm={(kubeconfig, contextName) => create.mutate({ kubeconfig, contextName })}
                />
              </>
            ) : (
              <p style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-muted)' }}>
                Name the cluster first, so there is something to call it once it connects.
              </p>
            )}
          </Step>
        </div>
      </div>
    </div>
  )
}

/**
 * A step, numbered.
 *
 * The number sits in the margin on a rule that runs the height of the step, so they read
 * as one sequence down the page rather than as unrelated cards.
 */
function Step({
  number,
  title,
  hint,
  muted = false,
  last = false,
  children,
}: {
  number: number
  title: string
  hint: string
  muted?: boolean
  /** The rule stops at the last step; below it there is nothing left to point at. */
  last?: boolean
  children: React.ReactNode
}) {
  return (
    <section className="flex gap-4" style={{ opacity: muted ? 0.55 : 1 }}>
      <span className="flex shrink-0 flex-col items-center gap-2">
        <span
          className="flex h-7 w-7 items-center justify-center font-mono"
          style={{
            fontSize: 'var(--text-micro)',
            borderRadius: '50%',
            border: '1px solid var(--border-strong)',
            color: 'var(--text-secondary)',
          }}
        >
          {number}
        </span>
        {!last && (
          <span aria-hidden="true" className="w-px flex-1" style={{ backgroundColor: 'var(--border-subtle)' }} />
        )}
      </span>

      <div className="min-w-0 flex-1 pb-2">
        <h3 style={{ fontSize: 'var(--text-body)', fontWeight: 600, color: 'var(--text-primary)' }}>{title}</h3>
        <p className="mb-3 mt-0.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {hint}
        </p>
        {children}
      </div>
    </section>
  )
}
