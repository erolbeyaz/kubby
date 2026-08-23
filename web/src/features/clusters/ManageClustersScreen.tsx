import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { ApiError, api, type Cluster, type Me } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

import { AddClusterForm } from './AddClusterForm'
import { ClusterDetail } from './ClusterDetail'
import { StatusBadge } from './StatusBadge'
import { environmentColor } from './environment'

interface ManageClustersScreenProps {
  me: Me
  onOpenCluster: (clusterId: string) => void
}

/**
 * Registering and configuring clusters.
 *
 * Deliberately separate from browsing: adding a cluster is an occasional administrative
 * act, while switching between them is constant. Mixing the two put a setup form in the
 * path of everyday navigation.
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

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex h-11 shrink-0 items-center justify-between border-b px-4"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <h2 className="font-semibold" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
          Clusters
          <span className="ml-2 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            {list.length}
          </span>
        </h2>
        {canManage && (
          <Button variant="primary" onClick={() => setAdding((value) => !value)}>
            {adding ? 'Cancel' : 'Add cluster'}
          </Button>
        )}
      </header>

      {adding && (
        <div className="border-b p-5" style={{ borderColor: 'var(--border-subtle)' }}>
          <AddClusterForm onCreated={() => setAdding(false)} />
        </div>
      )}

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

        {!clusters.isLoading && list.length === 0 && !adding && (
          <EmptyState
            title="No clusters yet"
            description={
              canManage
                ? 'Paste a kubeconfig to connect your first cluster.'
                : 'No clusters have been shared with you yet.'
            }
          />
        )}

        {list.length > 0 && (
          <ClusterTable clusters={list} onOpen={onOpenCluster} onConfigure={setEditing} />
        )}
      </div>
    </div>
  )
}

function ClusterTable({
  clusters,
  onOpen,
  onConfigure,
}: {
  clusters: Cluster[]
  onOpen: (id: string) => void
  onConfigure: (id: string) => void
}) {
  const queryClient = useQueryClient()

  const test = useMutation({
    mutationFn: (id: string) => api.testCluster(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['clusters'] }),
  })

  return (
    <table className="w-full text-left" style={{ fontSize: 'var(--text-secondary-size)' }}>
      <thead>
        <tr style={{ color: 'var(--text-muted)' }}>
          <th className="px-4 py-2 font-medium">Cluster</th>
          <th className="px-4 py-2 font-medium">Status</th>
          <th className="px-4 py-2 font-medium">Version</th>
          <th className="px-4 py-2 font-medium">Nodes</th>
          <th className="px-4 py-2 font-medium">Checked</th>
          <th className="px-4 py-2" />
        </tr>
      </thead>
      <tbody>
        {clusters.map((cluster) => (
          <tr key={cluster.id} className="border-t" style={{ borderColor: 'var(--border-subtle)' }}>
            <td className="px-4 py-2.5">
              <button
                type="button"
                onClick={() => onOpen(cluster.id)}
                className="flex items-center gap-2.5 text-left hover:underline"
              >
                <span
                  aria-hidden="true"
                  className="inline-block h-7 w-1"
                  style={{ backgroundColor: environmentColor(cluster.environment, cluster.color) }}
                />
                <span>
                  <span className="block" style={{ color: 'var(--text-primary)' }}>
                    {cluster.name}
                    {cluster.readOnly && (
                      <span
                        className="ml-2 px-1.5 py-0.5 font-mono uppercase"
                        style={{
                          fontSize: 'var(--text-micro)',
                          borderRadius: 'var(--radius-sharp)',
                          backgroundColor: 'var(--bg-raised)',
                          color: 'var(--status-warn)',
                        }}
                      >
                        read-only
                      </span>
                    )}
                  </span>
                  <span
                    className="block font-mono"
                    style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
                  >
                    {cluster.displayEnvironment} · {cluster.apiServerUrl}
                  </span>
                </span>
              </button>
            </td>

            <td className="px-4 py-2.5">
              <StatusBadge status={cluster.credentialStatus} detail={cluster.statusDetail} />
            </td>

            <td className="px-4 py-2.5 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>
              {cluster.k8sVersion || '—'}
            </td>

            <td className="px-4 py-2.5 font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>
              {cluster.nodeCount ?? '—'}
            </td>

            <td
              className="px-4 py-2.5 font-mono"
              style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
              title={cluster.lastValidatedAt ? formatAbsolute(cluster.lastValidatedAt) : undefined}
            >
              {cluster.lastValidatedAt ? `${formatAge(cluster.lastValidatedAt)} ago` : 'never'}
            </td>

            <td className="px-4 py-2.5">
              <span className="flex justify-end gap-2">
                <Button
                  loading={test.isPending && test.variables === cluster.id}
                  onClick={() => test.mutate(cluster.id)}
                >
                  Test
                </Button>
                <Button onClick={() => onConfigure(cluster.id)}>Configure</Button>
                <Button variant="primary" onClick={() => onOpen(cluster.id)}>
                  Browse
                </Button>
              </span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
