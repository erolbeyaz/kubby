import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import type { Location } from '@/app/navigation'
import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { DockLauncher } from '@/components/DockLauncher'
import { ResizablePanel } from '@/components/ResizablePanel'
import { ApiError, api, type Cluster, type ResourceRow } from '@/lib/api'

import { Home } from '@/features/home/Home'
import { ClusterOverview2 } from '@/features/overview2/ClusterOverview2'

import { typeKeyForKind } from './statusColor'
import { ObjectDrawer } from './ObjectDrawer'
import { BottomDock } from '@/components/BottomDock'
import { HealthPanel } from '@/features/health/HealthPanel'
import { HelmReleases } from '@/features/helm/HelmReleases'
import { DescribePane } from '@/features/logs/DescribePane'
import { LogPane } from '@/features/logs/LogPane'
import { ContextMenu, type MenuItem } from '@/components/ContextMenu'
import { CreatePane } from '@/features/create/CreatePane'
import { TAB_ICONS, closeTab, openCreateTab, openTab, tabLabel, type DockTab } from '@/features/logs/dock'
import { ClusterTerminalPane } from '@/features/terminal/ClusterTerminalPane'
import { ShellPane } from '@/features/terminal/ShellPane'
import { TerminalPane } from '@/features/terminal/TerminalPane'
import { nodeShellPath } from '@/lib/exec-stream'

import { PortForwardDialog } from './PortForwardDialog'
import { PortForwards } from './PortForwards'

import { ActionRunner, type PendingAction } from './ActionRunner'
import { DrainDialog } from './DrainDialog'
import { ScaleDialog } from './ScaleDialog'
import { DeleteDialog, type DeleteTarget } from './DeleteDialog'
import { actionsFor } from './actions'

import { ResourceTable, type NavigationTarget } from './ResourceTable'
import { NavigationRail } from './NavigationRail'

interface ResourceExplorerProps {
  cluster: Cluster
  clusters: Cluster[]
  location: Location
  onNavigate: (next: Partial<Location>) => void
  canManage: boolean
  onSelectCluster: (clusterId: string) => void
  onManageClusters: () => void
  /**
   * The status strip, rendered inside this column so the rail reaches the bottom. It is
   * given the dock's + to carry, which is why it arrives as a function rather than an
   * element: the + belongs to this screen, the strip does not.
   */
  footer?: (leading: React.ReactNode) => React.ReactNode
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
  footer,
}: ResourceExplorerProps) {
  const queryClient = useQueryClient()
  const [clickedRow, setClickedRow] = useState<ResourceRow | null>(null)
  // The dock belongs to the reader, not to whatever row is selected. Opening a second
  // pod's log is a second tab, so the first is still there to compare against.
  const [dock, setDock] = useState<{ tabs: DockTab[]; activeId: string }>({ tabs: [], activeId: '' })
  const [menu, setMenu] = useState<{ row: ResourceRow; at: { x: number; y: number } } | null>(null)
  // Selection belongs to the view, not the session: keeping it across a change of kind
  // or namespace would let someone delete what they are no longer looking at.
  const [selection, setSelection] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState<DeleteTarget | null>(null)
  const [pending, setPending] = useState<PendingAction | null>(null)
  const [scaling, setScaling] = useState<ResourceRow | null>(null)
  const [draining, setDraining] = useState<string | null>(null)
  const [forwarding, setForwarding] = useState<ResourceRow | null>(null)
  // Open tunnels, keyed by the tab showing them. The session lives on the server; this is
  // only what the tab needs to render it.
  // A write sets off a short window of close attention, so what the cluster does next is
  // watched rather than discovered fifteen seconds later.
  const [live, setLive] = useState(false)
  const settle = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => {
    if (settle.current) clearTimeout(settle.current)
  }, [])

  const watchForChanges = () => {
    setLive(true)
    if (settle.current) clearTimeout(settle.current)
    settle.current = setTimeout(() => setLive(false), SETTLE_WINDOW)
  }

  // The table owns the rows, so it hands back the ones it selected rather than this
  // reaching into its query cache with a key it would have to keep in step.
  const runAction = (id: string, row: ResourceRow) => {
    switch (id) {
      case 'details':
        openObject(row)
        return
      case 'delete':
        openDelete([row])
        return
      case 'scale':
        setScaling(row)
        return
      case 'drain':
        setDraining(row.name)
        return
      case 'edit':
        setDock((current) =>
          openTab(current.tabs, {
            kind: 'edit',
            clusterId: cluster.id,
            typeKey: location.typeKey,
            namespace: row.namespace ?? '',
            name: row.name,
          }),
        )
        return
      case 'logs':
      case 'describe':
        openDock(id, row)
        return
      case 'shell':
        openDock('shell', row)
        return
      case 'node-shell':
        openDock('node-shell', row)
        return
      case 'forward':
        setForwarding(row)
        return
      default:
        setPending({ id, clusterId: cluster.id, typeKey: location.typeKey, kind, row })
    }
  }

  // What the dock's + offers. Terminal first: it is the one opened by reflex, and a
  // manifest is written far less often than a command is run.
  const newTabItems = [
    {
      id: 'terminal',
      label: 'Terminal',
      onSelect: () =>
        setDock((current) =>
          openTab(current.tabs, {
            kind: 'terminal',
            clusterId: cluster.id,
            typeKey: '',
            namespace: '',
            name: cluster.name,
          }),
        ),
    },
    {
      id: 'create',
      label: 'Create Resource',
      onSelect: () => setDock((current) => openCreateTab(current, cluster.id, location.typeKey)),
    },
  ]

  const openDelete = (rows: ResourceRow[]) => {
    if (rows.length === 0) return
    setDeleting({ clusterId: cluster.id, typeKey: location.typeKey, kind, rows })
  }

  const openDock = (kind: 'logs' | 'describe' | 'shell' | 'node-shell', row: ResourceRow) =>
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

  // Clicking a row while the panel is open closes it, whichever row it is. Asked for
  // directly: the panel is dismissed by clicking anywhere in the list, not only by
  // finding an empty patch of it.
  const openObject = (row: ResourceRow) => {
    if (location.objectName) {
      onNavigate({ objectName: null, objectNamespace: '' })
      return
    }

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
        <NavigationRail
          clusters={clusters}
          current={cluster}
          canManage={canManage}
          selectedType={location.typeKey}
          onSelectCluster={onSelectCluster}
          onManageClusters={onManageClusters}
          onSelectType={(typeKey) =>
            typeKey === 'kubby-settings'
              ? onNavigate({ section: 'settings', settingsView: 'kubby' })
              : onNavigate({ typeKey, objectName: null, objectNamespace: '' })
          }
        />
      </ResizablePanel>

      {/* A column rather than a row: the status strip belongs under this side of the
          split, so the rail beside it runs the full height of the window. */}
      <div className="flex min-w-0 flex-1 flex-col">
        <main className="flex min-h-0 min-w-0 flex-1">
        {namespacesError ? (
          <div className="p-4">
            <Callout tone="error" title="Could not read from this cluster" requestId={namespacesError.requestId}>
              {namespacesError.message}
            </Callout>
          </div>
        ) : location.typeKey === 'overview2' ? (
          <div className="min-w-0 flex-1">
            <ClusterOverview2
              cluster={cluster}
              onOpenObject={(kind, namespace, name) => {
                const typeKey = typeKeyForKind(kind)
                if (!typeKey) return
                if (kind === 'Namespace') return navigateTo({ typeKey: 'pods', namespace: name })
                navigateTo({ typeKey, namespace, objectName: name })
              }}
              onOpenType={(typeKey, namespace) =>
                navigateTo({ typeKey, namespace: namespace ?? '', objectName: null })
              }
              // Not onSelectCluster: that lands on the default Overview, so switching
              // cluster from the strip at the top of this screen threw the reader off
              // it. A card here changes which cluster, never which screen.
              onOpenCluster={(clusterId) =>
                onNavigate({
                  clusterId,
                  namespaces: [],
                  typeKey: 'overview2',
                  objectName: null,
                  objectNamespace: '',
                })
              }
            />
          </div>
        ) : location.typeKey === 'home' ? (
          <div className="min-w-0 flex-1">
            <Home
              onOpen={(clusterId) =>
                onNavigate({
                  clusterId,
                  namespaces: [],
                  typeKey: 'overview2',
                  objectName: null,
                  objectNamespace: '',
                })
              }
            />
          </div>
        ) : location.typeKey === 'applications' ? (
          <div className="min-w-0 flex-1">
            <HelmReleases clusterId={cluster.id} namespaces={location.namespaces} />
          </div>
        ) : location.typeKey === 'port-forwards' ? (
          <div className="min-w-0 flex-1">
            <PortForwards clusterId={cluster.id} />
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
                selection={selection}
                onSelectionChange={setSelection}
                canWrite={canManage}
                onCreate={() => setDock((current) => openCreateTab(current, cluster.id, location.typeKey))}
                onDeleteSelected={openDelete}
                live={live}
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
                      onAction={(id) => runAction(id, selectedRow)}
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
                newTabItems={newTabItems}
                tabs={dock.tabs.map((tab) => ({
                  id: tab.id,
                  label: tabLabel(tab),
                  icon: TAB_ICONS[tab.kind],
                  render: () => {
                    if (tab.kind === 'logs') {
                      return <LogPane clusterId={tab.clusterId} namespace={tab.namespace} pod={tab.name} />
                    }
                    if (tab.kind === 'terminal') {
                      return <ClusterTerminalPane clusterId={tab.clusterId} clusterName={tab.name} />
                    }
                    if (tab.kind === 'shell') {
                      return (
                        <ShellPane clusterId={tab.clusterId} namespace={tab.namespace} pod={tab.name} />
                      )
                    }
                    if (tab.kind === 'node-shell') {
                      return (
                        <TerminalPane
                          path={nodeShellPath(tab.clusterId, tab.name)}
                          opening={`Starting a privileged pod on ${tab.name}…`}
                        />
                      )
                    }
                    if (tab.kind === 'create' || tab.kind === 'edit') {
                      return (
                        <CreatePane
                          clusterId={tab.clusterId}
                          namespaces={location.namespaces}
                          onChanged={watchForChanges}
                          {...(tab.kind === 'edit'
                            ? {
                                editing: {
                                  typeKey: tab.typeKey,
                                  namespace: tab.namespace,
                                  name: tab.name,
                                },
                              }
                            : {})}
                        />
                      )
                    }
                    return (
                      <DescribePane
                        clusterId={tab.clusterId}
                        typeKey={tab.typeKey}
                        namespace={tab.namespace}
                        name={tab.name}
                      />
                    )
                  },
                }))}
              />
            )}
          </div>
        ) : (
          <EmptyState title="Pick a resource kind" description="Choose a kind from the panel to browse it." />
        )}
        </main>

        {footer?.(<DockLauncher items={newTabItems} />)}
      </div>

      {deleting && (
        <DeleteDialog
          target={deleting}
          onChanged={watchForChanges}
          onClose={() => {
            setDeleting(null)
            setSelection(new Set())
          }}
        />
      )}

      {scaling && (
        <ScaleDialog
          clusterId={cluster.id}
          typeKey={location.typeKey}
          kind={kind}
          row={scaling}
          onChanged={watchForChanges}
          onClose={() => setScaling(null)}
        />
      )}

      {forwarding && (
        <PortForwardDialog
          clusterId={cluster.id}
          typeKey={location.typeKey}
          name={forwarding.name}
          namespace={forwarding.namespace ?? ''}
          onOpened={() => {
            // Nothing to open here: the tunnel is a browser tab now, and the chip in the
            // toolbar is where it is stopped.
            void queryClient.invalidateQueries({ queryKey: ['forwards', cluster.id] })
          }}
          onClose={() => setForwarding(null)}
        />
      )}

      {draining && (
        <DrainDialog
          clusterId={cluster.id}
          node={draining}
          onChanged={watchForChanges}
          onClose={() => setDraining(null)}
        />
      )}

      {pending && (
        <ActionRunner
          action={pending}
          onChanged={watchForChanges}
          onClose={() => setPending(null)}
        />
      )}

      {menu && (
        <ContextMenu
          at={menu.at}
          onClose={() => setMenu(null)}
          items={menuItemsFor(kind, (id) => runAction(id, menu.row))}
        />
      )}
    </div>
  )
}

/**
 * How long the list watches closely after a write.
 *
 * Long enough for a controller to react and for its replacement to be scheduled; short
 * enough that a forgotten tab is not polling every second all afternoon.
 */
const SETTLE_WINDOW = 20_000

/**
 * The right-click menu, built from the one action registry (ADR-053).
 *
 * Actions a later phase brings are listed and disabled rather than hidden: the shape of
 * what an object can do is part of understanding it, and a menu that grows silently
 * between releases teaches people to stop looking.
 */
function menuItemsFor(kind: string, run: (id: string) => void): MenuItem[] {
  return actionsFor(kind).map((action) => ({
    id: action.id,
    label: action.label,
    icon: action.icon,
    ...(action.destructive ? { destructive: true } : {}),
    ...(action.comingIn ? { disabled: true, note: action.comingIn } : {}),
    onSelect: () => run(action.id),
  }))
}
