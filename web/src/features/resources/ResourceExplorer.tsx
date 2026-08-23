import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import type { Location } from '@/app/navigation'
import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { ResizablePanel } from '@/components/ResizablePanel'
import { ApiError, api, type Cluster, type ResourceRow } from '@/lib/api'

import { ClusterOverview } from './ClusterOverview'
import { ObjectDrawer } from './ObjectDrawer'
import { ResourceTable, type NavigationTarget } from './ResourceTable'
import { ResourceTree } from './ResourceTree'

interface ResourceExplorerProps {
  cluster: Cluster
  location: Location
  onNavigate: (next: Partial<Location>) => void
  canManage: boolean
}

/**
 * Browsing one cluster.
 *
 * Namespaces, kind and the open object all live in the URL, so back steps through the
 * browsing history rather than jumping out of the cluster entirely.
 */
export function ResourceExplorer({ cluster, location, onNavigate }: ResourceExplorerProps) {
  const queryClient = useQueryClient()
  const [clickedRow, setClickedRow] = useState<ResourceRow | null>(null)

  const navigateTo = (target: NavigationTarget) => {
    const next: Partial<Location> = {}
    if (target.typeKey !== undefined) next.typeKey = target.typeKey
    if (target.namespace !== undefined) next.namespaces = target.namespace ? [target.namespace] : []
    if (target.objectName !== undefined) {
      next.objectName = target.objectName
      // A link that opens an object names the namespace that object lives in, which is
      // not necessarily the one the list is filtered to.
      next.objectNamespace = target.objectName ? (target.namespace ?? '') : ''
    }
    onNavigate(next)
  }

  const namespaces = useQuery({
    queryKey: ['namespaces', cluster.id],
    queryFn: ({ signal }) => api.namespaces(cluster.id, signal),
    staleTime: 60_000,
    enabled: cluster.credentialStatus === 'valid',
  })

  const types = useQuery({
    queryKey: ['resource-types', cluster.id],
    queryFn: ({ signal }) => api.resourceTypes(cluster.id, signal),
    staleTime: 5 * 60_000,
    enabled: cluster.credentialStatus === 'valid',
  })

  const currentType = types.data?.types.find((t) => t.key === location.typeKey)
  const kind = currentType?.kind ?? location.typeKey
  const scopedNamespaces = currentType?.namespaced ? location.namespaces : []

  if (cluster.credentialStatus !== 'valid') {
    return (
      <div className="p-5">
        <Callout
          tone={cluster.credentialStatus === 'invalid' ? 'error' : 'warning'}
          title={
            cluster.credentialStatus === 'invalid'
              ? "This cluster's credential no longer works"
              : 'Cluster unreachable'
          }
        >
          {cluster.statusDetail || 'Kubby cannot read from this cluster right now.'}
        </Callout>
      </div>
    )
  }

  const namespacesError = namespaces.error instanceof ApiError ? namespaces.error : null

  const openObject = (row: ResourceRow) => {
    setClickedRow(row)
    onNavigate({ objectName: row.name, objectNamespace: row.namespace ?? '' })
  }

  // Prefetching on hover means the object is usually already in cache by the time the
  // row is clicked, so the panel opens with content rather than a placeholder.
  const prefetch = (row: ResourceRow) => {
    void queryClient.prefetchQuery({
      queryKey: ['object', cluster.id, location.typeKey, row.namespace ?? '', row.name],
      queryFn: ({ signal }) =>
        api.resourceObject(
          cluster.id,
          location.typeKey,
          { name: row.name, ...(row.namespace ? { namespace: row.namespace } : {}) },
          signal,
        ),
      staleTime: 10_000,
    })
  }

  const selectedRow: ResourceRow | null = location.objectName
    ? clickedRow?.name === location.objectName
      ? clickedRow
      : {
          name: location.objectName,
          namespace: location.objectNamespace,
          age: '',
          createdAt: '',
          fields: {},
        }
    : null

  return (
    <div className="flex h-full">
      <ResizablePanel storageKey="kubby.explorer.width" defaultWidth={208} minWidth={168} maxWidth={380}>
        <div
          className="h-full border-r"
          style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
        >
          <ResourceTree
            clusterId={cluster.id}
            selectedType={location.typeKey}
            onSelectType={(typeKey) => onNavigate({ typeKey, objectName: null, objectNamespace: '' })}
          />
        </div>
      </ResizablePanel>

      <main className="flex min-w-0 flex-1">
        {namespacesError ? (
          <div className="p-4">
            <Callout tone="error" title="Could not read from this cluster" requestId={namespacesError.requestId}>
              {namespacesError.message}
            </Callout>
          </div>
        ) : location.typeKey === 'overview' ? (
          <div className="min-w-0 flex-1">
            <ClusterOverview cluster={cluster} onNavigate={navigateTo} />
          </div>
        ) : currentType ? (
          // The drawer floats over the list rather than sharing the row with it: making
          // the table narrower every time an object is opened reflows every column and
          // costs exactly the columns the object was opened to be compared against.
          <div className="relative min-w-0 flex-1">
            <div className="h-full min-w-0">
              <ResourceTable
                clusterId={cluster.id}
                typeKey={location.typeKey}
                kind={kind}
                namespaces={scopedNamespaces}
                allNamespaces={namespaces.data?.namespaces ?? []}
                namespaceScoped={currentType.namespaced}
                onNamespacesChange={(next) => onNavigate({ namespaces: next, objectName: null, objectNamespace: '' })}
                onOpen={openObject}
                onPrefetch={prefetch}
                onNavigate={navigateTo}
                selectedName={location.objectName}
                onDismiss={() => location.objectName && onNavigate({ objectName: null, objectNamespace: '' })}
              />
            </div>

            {selectedRow && (
              <div
                className="absolute inset-y-0 right-0 z-30 flex"
                style={{ boxShadow: 'var(--shadow-panel, -8px 0 24px rgb(0 0 0 / 0.35))' }}
              >
                <ResizablePanel
                  storageKey="kubby.drawer.width"
                  defaultWidth={400}
                  minWidth={300}
                  maxWidth={880}
                  side="right"
                >
                  <ObjectDrawer
                    clusterId={cluster.id}
                    typeKey={location.typeKey}
                    kind={kind}
                    row={selectedRow}
                    onClose={() => onNavigate({ objectName: null, objectNamespace: '' })}
                    onNavigate={navigateTo}
                  />
                </ResizablePanel>
              </div>
            )}
          </div>
        ) : (
          <EmptyState title="Pick a resource kind" description="Choose a kind from the panel to browse it." />
        )}
      </main>
    </div>
  )
}
