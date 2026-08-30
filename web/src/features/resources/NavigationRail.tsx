import { ClusterPicker } from '@/components/ClusterPicker'
import type { Cluster } from '@/lib/api'

import { ResourceTree } from './ResourceTree'

interface NavigationRailProps {
  clusters: Cluster[]
  /** Null before a cluster is chosen, and before any exists. */
  current: Cluster | null
  canManage: boolean
  selectedType: string | null
  onSelectCluster: (clusterId: string) => void
  onManageClusters: () => void
  onSelectType: (typeKey: string) => void
}

/**
 * The rail, wherever the reader is.
 *
 * Shared rather than drawn twice because it used to exist only inside the cluster
 * explorer, and the explorer needs a cluster: on a fresh install there was nothing to
 * open, so there was no rail, so the way to add a cluster was not on screen. A first-run
 * screen that hides its own navigation leaves the reader looking for a control nobody
 * drew.
 */
export function NavigationRail({
  clusters,
  current,
  canManage,
  selectedType,
  onSelectCluster,
  onManageClusters,
  onSelectType,
}: NavigationRailProps) {
  return (
    <div
      className="flex h-full flex-col border-r"
      style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
    >
      {/* Which cluster, at the top of the rail everything under it belongs to. The rule
          below it says where the cluster ends and its contents begin. */}
      <div className="shrink-0 border-b p-2" style={{ borderColor: 'var(--border-default)' }}>
        <ClusterPicker
          clusters={clusters}
          current={current}
          canManage={canManage}
          onSelect={onSelectCluster}
          onManage={onManageClusters}
          fullWidth
        />
      </div>

      <div className="min-h-0 flex-1">
        {/* An empty id lists no kinds and asks the server for none; the fixed entries
            below it are the ones that work without a cluster anyway. */}
        <ResourceTree
          clusterId={current?.id ?? ''}
          selectedType={selectedType}
          canManageSettings={canManage}
          onSelectType={onSelectType}
        />
      </div>
    </div>
  )
}
