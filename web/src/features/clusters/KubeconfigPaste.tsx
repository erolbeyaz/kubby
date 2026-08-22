import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'

import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { ApiError, api, type KubeContext, type ValidateResult } from '@/lib/api'

interface KubeconfigPasteProps {
  /** Called once a context has been validated and the user confirms it. */
  onConfirm: (kubeconfig: string, contextName: string, result: ValidateResult) => void
  confirmLabel: string
  busy?: boolean
}

/**
 * Paste, check, then commit.
 *
 * Nothing is stored until the user has seen what the credential actually is and what
 * it can do — the server validates without writing anything (ADR-018).
 */
export function KubeconfigPaste({ onConfirm, confirmLabel, busy }: KubeconfigPasteProps) {
  const [kubeconfig, setKubeconfig] = useState('')
  const [selected, setSelected] = useState('')

  const validate = useMutation({
    mutationFn: (contextName: string) => api.validateKubeconfig({ kubeconfig, contextName }),
    onSuccess: (result, contextName) => {
      if (!contextName) setSelected(result.currentContext)
    },
  })

  const error = validate.error instanceof ApiError ? validate.error : null
  const result = validate.data
  const chosen = result?.contexts.find((c) => c.name === (selected || result.currentContext))

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <label
          htmlFor="kubeconfig"
          className="font-medium"
          style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}
        >
          Kubeconfig
        </label>
        <textarea
          id="kubeconfig"
          spellCheck={false}
          rows={10}
          placeholder={'apiVersion: v1\nkind: Config\nclusters:\n  - name: …'}
          value={kubeconfig}
          onChange={(e) => {
            setKubeconfig(e.target.value)
            validate.reset()
            setSelected('')
          }}
          className="w-full resize-y border p-2.5 font-mono outline-none focus:border-[var(--accent)]"
          style={{
            fontSize: 'var(--text-micro)',
            lineHeight: 1.55,
            backgroundColor: 'var(--bg-base)',
            borderColor: 'var(--border-default)',
            borderRadius: 'var(--radius-sharp)',
            color: 'var(--text-primary)',
          }}
        />
        <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          Paste the whole file. It is checked before anything is saved, and stored
          encrypted — it is never shown again.
        </p>
      </div>

      {error && (
        <Callout tone="error" title="This kubeconfig cannot be used" requestId={error.requestId}>
          {error.message}
        </Callout>
      )}

      {!result && (
        <div>
          <Button
            variant="primary"
            disabled={kubeconfig.trim().length === 0}
            loading={validate.isPending}
            onClick={() => validate.mutate('')}
          >
            Check connection
          </Button>
        </div>
      )}

      {result && (
        <>
          <ContextPicker
            contexts={result.contexts}
            selected={selected || result.currentContext}
            currentContext={result.currentContext}
            onSelect={(name) => {
              setSelected(name)
              validate.mutate(name)
            }}
          />

          {chosen && <ContextWarnings context={chosen} />}
          {result.probe && <ProbeSummary probe={result.probe} />}

          <div className="flex gap-2">
            <Button
              variant="primary"
              loading={busy ?? false}
              disabled={!chosen || chosen.blocked}
              onClick={() => onConfirm(kubeconfig, selected || result.currentContext, result)}
            >
              {confirmLabel}
            </Button>
            <Button
              onClick={() => {
                validate.reset()
                setSelected('')
              }}
            >
              Start over
            </Button>
          </div>
        </>
      )}
    </div>
  )
}

function ContextPicker({
  contexts,
  selected,
  currentContext,
  onSelect,
}: {
  contexts: KubeContext[]
  selected: string
  currentContext: string
  onSelect: (name: string) => void
}) {
  if (contexts.length === 1) return null

  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend
        className="mb-1 font-medium"
        style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}
      >
        This kubeconfig has {contexts.length} contexts — pick one
      </legend>

      {contexts.map((context) => (
        <label
          key={context.name}
          className="flex cursor-pointer items-start gap-2.5 border p-2.5"
          style={{
            borderColor: context.name === selected ? 'var(--accent)' : 'var(--border-subtle)',
            borderRadius: 'var(--radius-sharp)',
            backgroundColor: context.name === selected ? 'var(--accent-muted)' : 'transparent',
            opacity: context.blocked ? 0.55 : 1,
          }}
        >
          <input
            type="radio"
            name="context"
            className="mt-1"
            checked={context.name === selected}
            disabled={context.blocked}
            onChange={() => onSelect(context.name)}
          />
          <span className="min-w-0 flex-1">
            <span className="flex flex-wrap items-center gap-2">
              <span style={{ color: 'var(--text-primary)' }}>{context.name}</span>
              {context.name === currentContext && (
                <span style={{ fontSize: 'var(--text-micro)', color: 'var(--accent)' }}>current</span>
              )}
              <span className="font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                {context.authMethod}
              </span>
            </span>
            <span
              className="mt-0.5 block truncate font-mono"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            >
              {context.server}
            </span>
            {context.problem && (
              <span
                className="mt-1 block"
                style={{
                  fontSize: 'var(--text-micro)',
                  color: context.blocked ? 'var(--status-error)' : 'var(--status-warn)',
                }}
              >
                {context.blocked ? 'Refused: ' : 'Warning: '}
                {context.problem}
              </span>
            )}
          </span>
        </label>
      ))}
    </fieldset>
  )
}

function ContextWarnings({ context }: { context: KubeContext }) {
  if (!context.insecureSkipTlsVerify && context.hasCertificateAuthority) return null

  return (
    <Callout tone="warning" title="Weaker transport security">
      {context.insecureSkipTlsVerify && (
        <p>
          This context sets <code>insecure-skip-tls-verify</code>. The connection is
          encrypted but the server's identity is not checked.
        </p>
      )}
      {!context.hasCertificateAuthority && !context.insecureSkipTlsVerify && (
        <p>This context carries no certificate authority, so the system trust store is used.</p>
      )}
    </Callout>
  )
}

/** What the credential actually is, before the user commits to storing it. */
function ProbeSummary({ probe }: { probe: NonNullable<ValidateResult['probe']> }) {
  if (probe.status !== 'valid') {
    return (
      <Callout
        tone={probe.status === 'invalid' ? 'error' : 'warning'}
        title={probe.status === 'invalid' ? 'The credential was rejected' : 'Cluster unreachable'}
      >
        {probe.detail ?? 'No further detail was returned.'}
      </Callout>
    )
  }

  return (
    <div
      className="border p-3"
      style={{
        borderColor: 'var(--status-ok)',
        borderRadius: 'var(--radius-sharp)',
        backgroundColor: 'var(--bg-raised)',
      }}
    >
      <p className="mb-2 font-medium" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--status-ok)' }}>
        Connected
      </p>

      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1" style={{ fontSize: 'var(--text-micro)' }}>
        <dt style={{ color: 'var(--text-muted)' }}>Version</dt>
        <dd className="font-mono" style={{ color: 'var(--text-primary)' }}>
          {probe.k8sVersion ?? 'unknown'}
        </dd>

        <dt style={{ color: 'var(--text-muted)' }}>Nodes</dt>
        <dd className="font-mono" style={{ color: 'var(--text-primary)' }}>
          {probe.nodeCount ?? '—'}
        </dd>

        <dt style={{ color: 'var(--text-muted)' }}>Metrics API</dt>
        <dd style={{ color: probe.metricsAvailable ? 'var(--text-primary)' : 'var(--status-warn)' }}>
          {probe.metricsAvailable ? 'available' : 'not installed — usage figures will be unavailable'}
        </dd>
      </dl>

      {probe.permissions && probe.permissions.length > 0 && (
        <div className="mt-2.5">
          <p className="mb-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            This credential can:
          </p>
          <div className="flex flex-wrap gap-1">
            {probe.permissions.map((permission) => (
              <span
                key={permission}
                className="px-1.5 py-0.5 font-mono"
                style={{
                  fontSize: 'var(--text-micro)',
                  borderRadius: 'var(--radius-sharp)',
                  backgroundColor: 'var(--bg-overlay)',
                  color: 'var(--text-secondary)',
                }}
              >
                {permission}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
