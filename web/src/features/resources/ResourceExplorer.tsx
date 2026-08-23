import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import type { Location } from '@/app/navigation'
import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { ResizablePanel } from '@/components/ResizablePanel'
import { ApiError, api, type Cluster, type ResourceRow } from '@/lib/api'

import { ClusterOverview } from './ClusterOverview'
import { ObjectDrawer } from './ObjectDrawer'
import { BottomDock } from '@/components/BottomDock'
import { HealthPanel } from '@/features/health/HealthPanel'
import { DescribePane } from '@/features/logs/DescribePane'
import { LogPane } from '@/features/logs/LogPane'
import { ClusterPicker } from '@/components/ClusterPicker'
import { ContextMenu, type MenuItem } from '@/components/ContextMenu'
import { TAB_ICONS, closeTab, openTab, tabLabel, type DockTab } from '@/features/logs/dock'

import { actionsFor } from './actions'

import { ResourceTable, type NavigationTarget } from './ResourceTable'
import { ResourceTree } from './ResourceTree'

interface ResourceExplorerProps {
  cluster: Cluster
  clusters: Cluster[]
  location: Location
  onNavigate: (next: Partial<Location>) => void
  canManage: boolean
  onSelectCluster: (clusterId: string) => void
  onManageClusters: () => void
}

/**
 * Browsing one cluster.
 *
 * Namespaces, kind and the open object all live in the URL, so back steps through the
 * browsing history rather than jumping out of the cluster entirely.
 */
export function ResourceExplorer({
  cluster,
  clusters,
  location,
  onNavigate,
  canManage,
  onSelectCluster,
  onManageClusters,
}: ResourceExplorerProps) {
  const queryClient = useQueryClient()
  const [clickedRow, setClickedRow] = useState<ResourceRow | null>(null)
  // The dock belongs to the reader, not to whatever row is selected. Opening a second
  // pod's log is a second tab, so the first is still there to compare against.
  const [dock, setDock] = useState<{ tabs: DockTab[]; activeId: string }>({ tabs: [], activeId: '' })
  const [menu, setMenu] = useState<{ row: ResourceRow; at: { x: number; y: number } } | null>(null)

  const openDock = (kind: 'logs' | 'describe', row: ResourceRow) =>
    setDock((current) =>
      openTab(current.tabs, {
        kind,
        clusterId: cluster.id,
        typeKey: location.typeKey,
        namespace: row.namespace ?? '',
        name: row.name,
      }),
    )

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
          className="flex h-full flex-col border-r"
          style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
        >
          {/* Which cluster, at the top of the rail everything under it belongs to.
              The rule below it says where the cluster ends and its contents begin. */}
          <div className="shrink-0 border-b p-2" style={{ borderColor: 'var(--border-default)' }}>
            <ClusterPicker
              clusters={clusters}
              current={cluster}
              canManage={canManage}
              onSelect={onSelectCluster}
              onManage={onManageClusters}
              fullWidth
            />
          </div>

          <div className="min-h-0 flex-1">
            <ResourceTree
              clusterId={cluster.id}
              selectedType={location.typeKey}
              onSelectType={(typeKey) => onNavigate({ typeKey, objectName: null, objectNamespace: '' })}
            />
          </div>
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
            <ClusterOverview cluster={cluster} onNavigate={navigateTo} onOpenCluster={onSelectCluster} />
          </div>
        ) : location.typeKey === 'health' ? (
          <div className="min-w-0 flex-1">
            <HealthPanel
              clusterId={cluster.id}
              namespaces={location.namespaces}
              // A finding is only useful if it leads somewhere: opening one lands on the
              // object it is about, with its namespace, rather than on a list to search.
              onOpen={(finding) =>
                onNavigate({
                  typeKey: finding.typeKey || 'pods',
                  namespaces: finding.namespace ? [finding.namespace] : [],
                  objectName: finding.name,
                  objectNamespace: finding.namespace ?? '',
                })
              }
            />
          </div>
        ) : currentType ? (
          // The drawer floats over the list rather than sharing the row with it: making
          // the table narrower every time an object is opened reflows every column and
          // costs exactly the columns the object was opened to be compared against.
          <div className="relative flex min-w-0 flex-1 flex-col">
            {/* The list area the drawer floats over; the dock sits below it and stays
                visible, because reading a log means looking at both. */}
            <div className="relative min-h-0 flex-1">
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
                onContextMenu={(row, at) => setMenu({ row, at })}
              />

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
                      onOpenDock={(kind) => openDock(kind, selectedRow)}
                    />
                  </ResizablePanel>
                </div>
              )}
            </div>

            {dock.tabs.length > 0 && (
              <BottomDock
                storageKey="kubby.dock.height"
                activeId={dock.activeId}
                onSelect={(id) => setDock((current) => ({ ...current, activeId: id }))}
                onCloseTab={(id) =>
                  setDock((current) => closeTab(current.tabs, id, current.activeId))
                }
                onClose={() => setDock({ tabs: [], activeId: '' })}
                tabs={dock.tabs.map((tab) => ({
                  id: tab.id,
                  label: tabLabel(tab),
                  icon: TAB_ICONS[tab.kind],
                  render: () =>
                    tab.kind === 'logs' ? (
                      <LogPane clusterId={tab.clusterId} namespace={tab.namespace} pod={tab.name} />
                    ) : (
                      <DescribePane
                        clusterId={tab.clusterId}
                        typeKey={tab.typeKey}
                        namespace={tab.namespace}
                        name={tab.name}
                      />
                    ),
                }))}
              />
            )}
          </div>
        ) : (
          <EmptyState title="Pick a resource kind" description="Choose a kind from the panel to browse it." />
        )}
      </main>

      {menu && (
        <ContextMenu
          at={menu.at}
          onClose={() => setMenu(null)}
          items={menuItemsFor(kind, {
            onDetails: () => openObject(menu.row),
            onDock: (dockKind) => openDock(dockKind, menu.row),
          })}
        />
      )}
    </div>
  )
}

/**
 * The right-click menu, built from the one action registry (ADR-053).
 *
 * Actions a later phase brings are listed and disabled rather than hidden: the shape of
 * what an object can do is part of understanding it, and a menu that grows silently
 * between releases teaches people to stop looking.
 */
function menuItemsFor(
  kind: string,
  handlers: { onDetails: () => void; onDock: (kind: 'logs' | 'describe') => void },
): MenuItem[] {
  return actionsFor(kind).map((action) => ({
    id: action.id,
    label: action.label,
    icon: action.icon,
    ...(action.destructive ? { destructive: true } : {}),
    ...(action.comingIn ? { disabled: true, note: action.comingIn } : {}),
    onSelect: () => {
      if (action.id === 'details') handlers.onDetails()
      else if (action.dockTab) handlers.onDock(action.dockTab)
    },
  }))
}
