import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { Icon } from '@/components/Icon'
import { ApiError, api, type Cluster, type Environment, type Me } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

import { AddClusterForm } from './AddClusterForm'
import { ClusterDetail } from './ClusterDetail'
import { StatusBadge } from './StatusBadge'
import { ENVIRONMENT_TITLE, byEnvironment, environmentColor } from './environment'

interface ManageClustersScreenProps {
  me: Me
  onOpenCluster: (clusterId: string) => void
}

/**
 * The cluster registry.
 *
 * Deliberately separate from browsing: adding a cluster is an occasional administrative
 * act, while switching between them is constant. Mixing the two put a setup form in the
 * path of everyday navigation.
 *
 * The page is grouped by environment rather than listed flat, and production is always
 * first. That is not decoration: environment is the one fact here that changes what the
 * tool will let you do (ADR-024, ADR-029), and the question somebody opens this screen
 * with is "is anything in production broken", not "how many clusters are there". A flat
 * table answered the second question and buried the first in a status column.
 */
export function ManageClustersScreen({ me, onOpenCluster }: ManageClustersScreenProps) {
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)

  const canManage = me.permissions.includes('cluster.manage')

  const clusters = useQuery({
    queryKey: ['clusters'],
    queryFn: ({ signal }) => api.clusters(signal),
    refetchInterval: 30_000,
  })

  const list = clusters.data?.clusters ?? []
  const current = list.find((c) => c.id === editing)

  if (current) {
    return <ClusterDetail cluster={current} canManage={canManage} onBack={() => setEditing(null)} />
  }

  if (adding) {
    return <AddClusterForm onDone={() => setAdding(false)} />
  }

  const broken = list.filter((cluster) => cluster.credentialStatus !== 'valid')

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex h-12 shrink-0 items-center justify-between gap-3 border-b px-4"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <h2 className="flex items-baseline gap-2">
          <span className="font-semibold" style={{ fontSize: 'var(--text-title)', color: 'var(--text-primary)' }}>
            Clusters
          </span>
          <span className="font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            {list.length === 0 ? '' : `${list.length} registered`}
          </span>
        </h2>

        {canManage && (
          <Button variant="primary" onClick={() => setAdding(true)}>
            <Icon name="plus" />
            Connect a cluster
          </Button>
        )}
      </header>

      <div className="min-h-0 flex-1 overflow-auto">
        {clusters.isLoading && (
          <p className="p-4" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-muted)' }}>
            Loading clusters…
          </p>
        )}

        {clusters.isError && (
          <div className="p-4">
            <Callout tone="error" title="Could not load clusters">
              {clusters.error instanceof ApiError ? clusters.error.message : 'Unexpected error'}
            </Callout>
          </div>
        )}

        {!clusters.isLoading && list.length === 0 && (
          <FirstCluster canManage={canManage} onStart={() => setAdding(true)} />
        )}

        {list.length > 0 && (
          <>
            <Attention clusters={broken} onOpen={setEditing} />
            <Registry clusters={list} onOpen={onOpenCluster} onConfigure={setEditing} />
          </>
        )}
      </div>
    </div>
  )
}

/**
 * What is broken, above what exists.
 *
 * The status column already carried this, but a column is read row by row: on a fleet of
 * eight, the one cluster that stopped answering is one line among eight identical-looking
 * ones. Named at the top, it is the first thing on the page — and it disappears entirely
 * when there is nothing to say, so its presence is itself the signal.
 */
function Attention({ clusters, onOpen }: { clusters: Cluster[]; onOpen: (id: string) => void }) {
  if (clusters.length === 0) return null

  return (
    <div
      className="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-b px-4 py-2.5"
      style={{
        borderColor: 'var(--border-subtle)',
        backgroundColor: 'color-mix(in srgb, var(--status-error) 8%, transparent)',
      }}
    >
      <span
        className="flex items-center gap-1.5 font-medium"
        style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--status-error)' }}
      >
        <Icon name="warning" />
        {clusters.length} cluster{clusters.length === 1 ? '' : 's'} need
        {clusters.length === 1 ? 's' : ''} attention
      </span>

      {clusters.map((cluster) => (
        <button
          key={cluster.id}
          type="button"
          onClick={() => onOpen(cluster.id)}
          className="flex min-w-0 items-baseline gap-1.5 hover:underline"
          style={{ fontSize: 'var(--text-micro)' }}
        >
          <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
            {cluster.name}
          </span>
          <span className="truncate" style={{ color: 'var(--text-secondary)' }}>
            {cluster.statusDetail || REASON[cluster.credentialStatus]}
          </span>
        </button>
      ))}
    </div>
  )
}

const REASON: Record<string, string> = {
  invalid: 'the credential was rejected',
  unreachable: 'the API server did not answer',
  unknown: 'never checked',
  valid: '',
}

/** The registry itself: one band per environment, production first. */
function Registry({
  clusters,
  onOpen,
  onConfigure,
}: {
  clusters: Cluster[]
  onOpen: (id: string) => void
  onConfigure: (id: string) => void
}) {
  const byTier = new Map<Environment, Cluster[]>()
  for (const cluster of clusters) {
    byTier.set(cluster.environment, [...(byTier.get(cluster.environment) ?? []), cluster])
  }

  const tiers = [...byTier.entries()].sort(([a], [b]) => byEnvironment(a, b))

  return (
    <div className="flex flex-col">
      {tiers.map(([environment, members]) => (
        <section key={environment}>
          <Band environment={environment} clusters={members} />
          {[...members]
            .sort((a, b) => a.name.localeCompare(b.name))
            .map((cluster) => (
              <Row
                key={cluster.id}
                cluster={cluster}
                onOpen={() => onOpen(cluster.id)}
                onConfigure={() => onConfigure(cluster.id)}
              />
            ))}
        </section>
      ))}
    </div>
  )
}

/**
 * The tier heading — the one piece of this page meant to be seen from across a desk.
 *
 * A solid cap in the tier's own colour, a hairline to the right edge, and the counts in
 * mono at the far end. Production's red band is the loudest thing on the screen on
 * purpose: mistaking which tier you are working in is the mistake this tool has to make
 * hardest (ADR-024).
 */
function Band({ environment, clusters }: { environment: Environment; clusters: Cluster[] }) {
  const colour = environmentColor(environment, clusters[0]?.color ?? '')
  // "Down" is a claim, and a cluster nobody has checked is not one Kubby can make about
  // it. The two are counted apart because they call for different things: one is a
  // cluster to go and fix, the other is a button to press.
  const down = clusters.filter(
    (cluster) => cluster.credentialStatus === 'invalid' || cluster.credentialStatus === 'unreachable',
  ).length
  const unchecked = clusters.filter((cluster) => cluster.credentialStatus === 'unknown').length

  return (
    <div className="flex items-center gap-3 px-4 pb-1.5 pt-5">
      <span
        className="px-2 py-0.5 font-semibold uppercase tracking-[0.1em]"
        style={{
          fontSize: 'var(--text-micro)',
          borderRadius: 'var(--radius-sharp)',
          backgroundColor: colour,
          color: 'var(--bg-base)',
        }}
      >
        {ENVIRONMENT_TITLE[environment]}
      </span>

      <span aria-hidden="true" className="h-px min-w-0 flex-1" style={{ backgroundColor: colour, opacity: 0.28 }} />

      <span className="font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {clusters.length} cluster{clusters.length === 1 ? '' : 's'}
        {down > 0 && (
          <>
            <span style={{ color: 'var(--border-strong)' }}> · </span>
            <span style={{ color: 'var(--status-error)' }}>{down} down</span>
          </>
        )}
        {unchecked > 0 && (
          <>
            <span style={{ color: 'var(--border-strong)' }}> · </span>
            <span style={{ color: 'var(--status-unknown)' }}>{unchecked} unchecked</span>
          </>
        )}
      </span>
    </div>
  )
}

/**
 * One cluster.
 *
 * The whole row opens it — the three buttons per row it replaces made every line look
 * like a decision to be made rather than a place to go. Configure and Test stay, but as
 * a hover affordance on the right, out of the way of the reading.
 */
function Row({
  cluster,
  onOpen,
  onConfigure,
}: {
  cluster: Cluster
  onOpen: () => void
  onConfigure: () => void
}) {
  const queryClient = useQueryClient()

  const test = useMutation({
    mutationFn: () => api.testCluster(cluster.id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['clusters'] }),
  })

  const colour = environmentColor(cluster.environment, cluster.color)

  return (
    <div
      className="group flex items-center gap-4 border-b px-4 py-2.5 transition-colors hover:bg-[var(--bg-hover)]"
      style={{ borderColor: 'var(--border-subtle)', borderLeft: `2px solid ${colour}` }}
    >
      <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-4 text-left">
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-2">
            <span className="truncate" style={{ fontSize: 'var(--text-body)', color: 'var(--text-primary)' }}>
              {cluster.name}
            </span>
            {cluster.readOnly && <Tag tone="var(--status-warn)">read-only</Tag>}
            {cluster.insecureSkipTlsVerify && <Tag tone="var(--status-warn)">tls unverified</Tag>}
          </span>
          <span
            className="mt-0.5 block truncate font-mono"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            title={cluster.apiServerUrl}
          >
            {cluster.apiServerUrl}
          </span>
        </span>

        <span className="w-40 shrink-0">
          <StatusBadge status={cluster.credentialStatus} detail={cluster.statusDetail} />
          <span
            className="mt-0.5 block truncate font-mono"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
            title={cluster.lastValidatedAt ? formatAbsolute(cluster.lastValidatedAt) : 'never checked'}
          >
            {cluster.lastValidatedAt ? `checked ${formatAge(cluster.lastValidatedAt)} ago` : 'never checked'}
          </span>
        </span>

        {/* Version and size read as one fact — what this cluster is — so they sit
            together rather than in two columns a reader has to join up. */}
        <span
          className="hidden w-32 shrink-0 font-mono sm:block"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
        >
          {cluster.k8sVersion || '—'}
          {cluster.nodeCount !== undefined && (
            <span style={{ color: 'var(--text-muted)' }}> · {cluster.nodeCount} nodes</span>
          )}
        </span>
      </button>

      {/* Quiet until wanted. Three buttons on every row made the list read as a form. */}
      <span className="flex shrink-0 gap-2 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
        <Button loading={test.isPending} onClick={() => test.mutate()}>
          Test
        </Button>
        <Button onClick={onConfigure}>Configure</Button>
      </span>
    </div>
  )
}

function Tag({ children, tone }: { children: React.ReactNode; tone: string }) {
  return (
    <span
      className="shrink-0 px-1.5 py-0.5 font-mono uppercase"
      style={{
        fontSize: 'var(--text-micro)',
        borderRadius: 'var(--radius-sharp)',
        backgroundColor: 'var(--bg-raised)',
        color: tone,
      }}
    >
      {children}
    </span>
  )
}

/** An empty screen is an invitation to act, not a notice that there is nothing here. */
function FirstCluster({ canManage, onStart }: { canManage: boolean; onStart: () => void }) {
  return (
    <div className="mx-auto flex max-w-lg flex-col items-start gap-3 px-4 py-16">
      <h3 style={{ fontSize: 'var(--text-title)', color: 'var(--text-primary)' }}>
        {canManage ? 'Connect your first cluster' : 'Nothing has been shared with you yet'}
      </h3>
      <p style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
        {canManage
          ? 'Paste a kubeconfig and Kubby will check it before storing anything. You choose which context to use, and the file is encrypted at rest — it is never shown again.'
          : 'Ask an administrator to grant you access to a cluster. It will appear here once they do.'}
      </p>
      {canManage && (
        <Button variant="primary" onClick={onStart}>
          <Icon name="plus" />
          Connect a cluster
        </Button>
      )}
    </div>
  )
}
