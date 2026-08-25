import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { Field, Select, TextInput } from '@/components/Field'
import { ApiError, api, type Cluster, type ClusterGrant } from '@/lib/api'
import { formatAbsolute } from '@/lib/time'

import { KubeconfigPaste } from './KubeconfigPaste'
import { StatusBadge } from './StatusBadge'
import { ENVIRONMENTS, ENVIRONMENT_HINT, environmentColor } from './environment'

interface ClusterDetailProps {
  cluster: Cluster
  canManage: boolean
  onBack: () => void
}

export function ClusterDetail({ cluster, canManage, onBack }: ClusterDetailProps) {
  const [replacing, setReplacing] = useState(false)

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex h-11 shrink-0 items-center gap-3 border-b px-4"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <Button onClick={onBack}>← Clusters</Button>
        <span
          aria-hidden="true"
          className="inline-block h-6 w-1"
          style={{ backgroundColor: environmentColor(cluster.environment, cluster.color) }}
        />
        <h2 className="font-semibold" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
          {cluster.name}
        </h2>
        <StatusBadge status={cluster.credentialStatus} detail={cluster.statusDetail} />
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        <div className="flex flex-col gap-5">
          {cluster.credentialStatus === 'invalid' && (
            <Callout tone="error" title="This cluster's credential no longer works">
              <p>{cluster.statusDetail || 'The credential was rejected by the API server.'}</p>
              {canManage && !replacing && (
                <div className="mt-2">
                  <Button variant="primary" onClick={() => setReplacing(true)}>
                    Update kubeconfig
                  </Button>
                </div>
              )}
            </Callout>
          )}

          {cluster.credentialStatus === 'unreachable' && (
            <Callout tone="warning" title="Cluster unreachable">
              {cluster.statusDetail ||
                'The API server did not answer. The credential itself may still be fine.'}
            </Callout>
          )}

          {cluster.insecureSkipTlsVerify && (
            <Callout tone="warning" title="TLS verification is disabled">
              This cluster was added with <code>insecure-skip-tls-verify</code>. Traffic is
              encrypted, but the API server's identity is not verified.
            </Callout>
          )}

          <Overview cluster={cluster} />

          {canManage && (
            <>
              <Panel title="Credential" description="Stored encrypted. Updating means pasting a new one.">
                {replacing ? (
                  <ReplaceCredential cluster={cluster} onDone={() => setReplacing(false)} />
                ) : (
                  <div className="flex items-center gap-3">
                    <Button onClick={() => setReplacing(true)}>Replace kubeconfig</Button>
                    <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                      The stored kubeconfig is never displayed, only replaced.
                    </span>
                  </div>
                )}
              </Panel>

              <SettingsPanel cluster={cluster} />
              <MetricsPanel cluster={cluster} />
              <ProtectionPanel cluster={cluster} />
              <AccessPanel cluster={cluster} />
              <DangerPanel cluster={cluster} onDeleted={onBack} />
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function Panel({
  title,
  description,
  tone,
  children,
}: {
  title: string
  description?: string
  tone?: 'danger'
  children: React.ReactNode
}) {
  return (
    <section
      className="border"
      style={{
        borderColor: tone === 'danger' ? 'var(--status-error)' : 'var(--border-subtle)',
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-panel)',
      }}
    >
      <header className="border-b px-4 py-2.5" style={{ borderColor: 'var(--border-subtle)' }}>
        <h3
          className="font-semibold"
          style={{
            fontSize: 'var(--text-secondary-size)',
            color: tone === 'danger' ? 'var(--status-error)' : 'var(--text-primary)',
          }}
        >
          {title}
        </h3>
        {description && (
          <p className="mt-0.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            {description}
          </p>
        )}
      </header>
      <div className="p-4">{children}</div>
    </section>
  )
}

function Overview({ cluster }: { cluster: Cluster }) {
  const rows: [string, string][] = [
    ['API server', cluster.apiServerUrl],
    ['Kubernetes', cluster.k8sVersion || 'unknown'],
    ['Nodes', cluster.nodeCount === undefined ? '—' : String(cluster.nodeCount)],
    ['Metrics API', cluster.metricsAvailable ? 'available' : 'not installed'],
    ['Environment', `${cluster.displayEnvironment} (${cluster.environment})`],
    ['Last checked', cluster.lastValidatedAt ? formatAbsolute(cluster.lastValidatedAt) : 'never'],
  ]

  return (
    <Panel title="Overview">
      <dl className="grid grid-cols-[10rem_1fr] gap-x-4 gap-y-1.5" style={{ fontSize: 'var(--text-secondary-size)' }}>
        {rows.map(([label, value]) => (
          <div key={label} className="contents">
            <dt style={{ color: 'var(--text-muted)' }}>{label}</dt>
            <dd className="truncate font-mono" style={{ color: 'var(--text-primary)' }} title={value}>
              {value}
            </dd>
          </div>
        ))}
      </dl>
    </Panel>
  )
}

function ReplaceCredential({ cluster, onDone }: { cluster: Cluster; onDone: () => void }) {
  const queryClient = useQueryClient()

  const replace = useMutation({
    mutationFn: (input: { kubeconfig: string; contextName: string }) =>
      api.replaceCredential(cluster.id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['clusters'] })
      onDone()
    },
  })

  const error = replace.error instanceof ApiError ? replace.error : null

  return (
    <div className="flex flex-col gap-3">
      {error && (
        <Callout tone="error" title="Could not replace the credential" requestId={error.requestId}>
          {error.message}
        </Callout>
      )}
      <KubeconfigPaste
        confirmLabel="Replace credential"
        busy={replace.isPending}
        onConfirm={(kubeconfig, contextName) => replace.mutate({ kubeconfig, contextName })}
      />
      <div>
        <Button onClick={onDone}>Cancel</Button>
      </div>
    </div>
  )
}

function SettingsPanel({ cluster }: { cluster: Cluster }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState(cluster.name)
  const [environment, setEnvironment] = useState(cluster.environment)
  const [environmentLabel, setEnvironmentLabel] = useState(cluster.environmentLabel)

  const update = useMutation({
    mutationFn: (body: Parameters<typeof api.updateCluster>[1]) => api.updateCluster(cluster.id, body),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['clusters'] }),
  })

  const error = update.error instanceof ApiError ? update.error : null
  const dirty =
    name !== cluster.name ||
    environment !== cluster.environment ||
    environmentLabel !== cluster.environmentLabel

  return (
    <Panel title="Settings">
      <div className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="Name">
            {(id) => <TextInput id={id} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label="Environment" hint={ENVIRONMENT_HINT[environment]}>
            {(id) => (
              <Select
                id={id}
                value={environment}
                onChange={(e) => setEnvironment(e.target.value as typeof environment)}
              >
                {ENVIRONMENTS.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Label">
            {(id) => (
              <TextInput
                id={id}
                value={environmentLabel}
                onChange={(e) => setEnvironmentLabel(e.target.value)}
              />
            )}
          </Field>
        </div>

        {error && (
          <Callout tone="error" title="Could not save" requestId={error.requestId}>
            {error.message}
          </Callout>
        )}

        {/* Save closes the panel's editable fields; it sits last so it clearly belongs
            to everything above it and to nothing else. */}
        <div
          className="flex items-center justify-end gap-3 border-t pt-4"
          style={{ borderColor: 'var(--border-subtle)' }}
        >
          {update.isSuccess && !dirty && (
            <span style={{ fontSize: 'var(--text-micro)', color: 'var(--status-ok)' }}>Saved</span>
          )}
          {dirty && (
            <Button
              onClick={() => {
                setName(cluster.name)
                setEnvironment(cluster.environment)
                setEnvironmentLabel(cluster.environmentLabel)
              }}
            >
              Discard
            </Button>
          )}
          <Button
            variant="primary"
            disabled={!dirty}
            loading={update.isPending}
            onClick={() => update.mutate({ name, environment, environmentLabel })}
          >
            Save changes
          </Button>
        </div>
      </div>
    </Panel>
  )
}

/**
 * The lock is its own panel because it applies immediately, unlike the fields above it.
 * Mixing an instant toggle with fields that need saving is what made the save button
 * look like it belonged to nothing.
 */
/**
 * Where this cluster's history is read from.
 *
 * Per cluster rather than one address for the whole deployment: Prometheus is normally
 * installed into the cluster it observes, so a fleet means one endpoint each. Answering
 * every cluster from a single address would show one cluster's numbers under every name,
 * which is worse than showing none. Left empty, the deployment-wide setting is used —
 * which is what a central Prometheus or a Thanos query layer looks like.
 */
function MetricsPanel({ cluster }: { cluster: Cluster }) {
  const queryClient = useQueryClient()
  const [url, setUrl] = useState(cluster.metricsUrl ?? '')
  const [username, setUsername] = useState(cluster.metricsUsername ?? '')
  const [password, setPassword] = useState('')
  const [insecure, setInsecure] = useState(cluster.metricsInsecureSkipVerify)

  const update = useMutation({
    mutationFn: (body: Parameters<typeof api.updateCluster>[1]) => api.updateCluster(cluster.id, body),
    onSuccess: () => {
      setPassword('')
      void queryClient.invalidateQueries({ queryKey: ['clusters'] })
      void queryClient.invalidateQueries({ queryKey: ['cluster-metrics'] })
    },
  })

  const error = update.error instanceof ApiError ? update.error : null
  const dirty =
    url !== (cluster.metricsUrl ?? '') ||
    username !== (cluster.metricsUsername ?? '') ||
    insecure !== cluster.metricsInsecureSkipVerify ||
    password !== ''

  return (
    <Panel
      title="Metrics"
      description="A Prometheus-compatible endpoint. It supplies the history the Kubernetes API cannot: whether restarts are accelerating, how long a workload has been degraded, how full the nodes' disks are."
    >
      <div className="flex flex-col gap-4">
        <Field
          label="Prometheus URL"
          hint="Its root, not a path. Empty falls back to the deployment-wide setting."
        >
          {(id) => (
            <TextInput
              id={id}
              value={url}
              placeholder="http://prometheus.monitoring.svc:9090"
              onChange={(e) => setUrl(e.target.value)}
            />
          )}
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Basic auth user" hint="Leave both empty if it needs no credentials.">
            {(id) => (
              <TextInput id={id} value={username} onChange={(e) => setUsername(e.target.value)} />
            )}
          </Field>
          <Field
            label="Password"
            // Empty means "leave what is stored", not "remove it": a form that wipes a
            // credential because it was not retyped is a form that loses credentials.
            hint={cluster.metricsUsername ? 'Stored. Type to replace it.' : 'Stored encrypted.'}
          >
            {(id) => (
              <TextInput
                id={id}
                type="password"
                value={password}
                autoComplete="new-password"
                onChange={(e) => setPassword(e.target.value)}
              />
            )}
          </Field>
        </div>

        <label className="flex items-center gap-2" style={{ fontSize: 'var(--text-micro)' }}>
          <input
            type="checkbox"
            checked={insecure}
            onChange={(e) => setInsecure(e.target.checked)}
          />
          <span style={{ color: 'var(--text-secondary)' }}>
            Accept a certificate this deployment does not trust
          </span>
        </label>

        {error && <p style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}>{error.message}</p>}

        <div>
          <Button
            onClick={() =>
              update.mutate({
                metricsUrl: url.trim(),
                metricsUsername: username.trim(),
                metricsInsecureSkipVerify: insecure,
                ...(password ? { metricsPassword: password } : {}),
              })
            }
            variant="primary"
            disabled={!dirty}
            loading={update.isPending}
          >
            Save
          </Button>
        </div>
      </div>
    </Panel>
  )
}

function ProtectionPanel({ cluster }: { cluster: Cluster }) {
  const queryClient = useQueryClient()

  const toggle = useMutation({
    mutationFn: (readOnly: boolean) => api.updateCluster(cluster.id, { readOnly }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['clusters'] }),
  })

  const error = toggle.error instanceof ApiError ? toggle.error : null

  return (
    <Panel title="Protection" description="Applies immediately.">
      <div className="flex flex-col gap-3">
        <label className="flex cursor-pointer items-start gap-2.5">
          <input
            type="checkbox"
            className="mt-1"
            checked={cluster.readOnly}
            disabled={toggle.isPending}
            onChange={(e) => toggle.mutate(e.target.checked)}
          />
          <span>
            <span className="block" style={{ color: 'var(--text-primary)' }}>
              Lock this cluster read-only
            </span>
            <span className="block" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
              Blocks changes to the cluster's own resources — scale, delete, apply, drain —
              for everyone, administrators included. Reading, logs and describe stay
              available, and this record can still be repaired or removed.
            </span>
          </span>
        </label>

        {error && (
          <Callout tone="error" title="Could not change the lock" requestId={error.requestId}>
            {error.message}
          </Callout>
        )}
      </div>
    </Panel>
  )
}

function AccessPanel({ cluster }: { cluster: Cluster }) {
  const queryClient = useQueryClient()

  const grants = useQuery({
    queryKey: ['cluster-grants', cluster.id],
    queryFn: ({ signal }) => api.clusterGrants(cluster.id, signal),
  })
  const users = useQuery({
    queryKey: ['users'],
    queryFn: ({ signal }) => api.users(signal),
  })

  const grantsKey = ['cluster-grants', cluster.id]
  const usersById = new Map((users.data?.users ?? []).map((u) => [u.id, u]))

  const setGrant = useMutation({
    mutationFn: (body: { userId: string; accessLevel: string }) =>
      api.setClusterGrant(cluster.id, body),

    // Show the new level immediately. Waiting for the round trip left the control
    // displaying the old value for a moment before jumping, which reads as the
    // dropdown fighting the user.
    onMutate: async ({ userId, accessLevel }) => {
      await queryClient.cancelQueries({ queryKey: grantsKey })
      const previous = queryClient.getQueryData<{ grants: ClusterGrant[] }>(grantsKey)
      const user = usersById.get(userId)

      queryClient.setQueryData<{ grants: ClusterGrant[] }>(grantsKey, (current) => {
        const others = (current?.grants ?? []).filter((g) => g.userId !== userId)
        if (accessLevel === '' || !user) return { grants: others }

        return {
          grants: [
            ...others,
            {
              userId,
              email: user.email,
              displayName: user.displayName,
              accessLevel: accessLevel as ClusterGrant['accessLevel'],
            },
          ],
        }
      })
      return { previous }
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(grantsKey, context.previous)
    },

    onSettled: () => void queryClient.invalidateQueries({ queryKey: grantsKey }),
  })

  const granted = new Map((grants.data?.grants ?? []).map((g) => [g.userId, g.accessLevel]))
  // Administrators reach every cluster through their role, so listing them here would
  // imply a grant that is not what actually gives them access.
  const candidates = (users.data?.users ?? []).filter((u) => u.role !== 'admin' && u.isActive)

  return (
    <Panel title="Access" description="Who may use this cluster. Administrators always have access.">
      {candidates.length === 0 ? (
        <p style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-muted)' }}>
          No non-administrator accounts exist yet.
        </p>
      ) : (
        <div className="flex flex-col gap-1.5">
          {candidates.map((user) => {
            const level = granted.get(user.id) ?? ''
            return (
              <div
                key={user.id}
                className="flex items-center justify-between gap-3 border-b pb-1.5"
                style={{ borderColor: 'var(--border-subtle)' }}
              >
                <span className="min-w-0">
                  <span className="block truncate" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
                    {user.displayName}
                  </span>
                  <span
                    className="block truncate font-mono"
                    style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
                  >
                    {user.email}
                  </span>
                </span>

                <Select
                  aria-label={`Access for ${user.email}`}
                  value={level}
                  onChange={(e) => setGrant.mutate({ userId: user.id, accessLevel: e.target.value })}
                >
                  <option value="">no access</option>
                  <option value="read">read</option>
                  <option value="write">write</option>
                </Select>
              </div>
            )
          })}
        </div>
      )}
    </Panel>
  )
}

function DangerPanel({ cluster, onDeleted }: { cluster: Cluster; onDeleted: () => void }) {
  const queryClient = useQueryClient()
  const [confirmation, setConfirmation] = useState('')

  const remove = useMutation({
    mutationFn: () => api.deleteCluster(cluster.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['clusters'] })
      onDeleted()
    },
  })

  // Production, or anything deliberately locked, demands the name typed out: a confirm
  // dialog is clicked through by muscle memory, and removing the wrong cluster is not
  // recoverable from the UI.
  const requiresTypedName = cluster.environment === 'prod' || cluster.readOnly
  const canDelete = !requiresTypedName || confirmation === cluster.name

  return (
    <Panel title="Remove cluster" description="Deletes the stored credential and access grants." tone="danger">
      <div className="flex flex-col gap-3">
        {requiresTypedName && (
          <Field label={`Type “${cluster.name}” to confirm`}>
            {(id) => (
              <TextInput
                id={id}
                value={confirmation}
                placeholder={cluster.name}
                onChange={(e) => setConfirmation(e.target.value)}
              />
            )}
          </Field>
        )}
        <div>
          <Button
            variant="danger"
            disabled={!canDelete}
            loading={remove.isPending}
            onClick={() => remove.mutate()}
          >
            Remove {cluster.name}
          </Button>
        </div>
      </div>
    </Panel>
  )
}
