import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { AccountMenu, type AccountAction } from '@/components/AccountMenu'
import { ClusterPicker } from '@/components/ClusterPicker'
import { EmptyState } from '@/components/EmptyState'
import { Logo } from '@/components/Logo'
import { StatusBar } from '@/components/StatusBar'
import { AccountScreen } from '@/features/account/AccountScreen'
import { ManageClustersScreen } from '@/features/clusters/ManageClustersScreen'
import { ResourceExplorer } from '@/features/resources/ResourceExplorer'
import { UsersScreen } from '@/features/users/UsersScreen'
import { api, type Me } from '@/lib/api'

import { useNavigation } from './navigation'
import { useServerStatus } from './use-server-status'

interface ShellProps {
  me: Me
  onSignOut: () => void
}

/**
 * The workspace.
 *
 * The cluster picker governs the window: everything below it is "within this cluster".
 * Navigation for a cluster's resources lives in the explorer's own panel, so the chrome
 * carries no menus that lead nowhere.
 */
export function Shell({ me, onSignOut }: ShellProps) {
  const { location, navigate } = useNavigation()
  const [accountFocus, setAccountFocus] = useState<AccountAction | null>(null)
  const { connection, detail, version } = useServerStatus()

  const canManage = me.permissions.includes('cluster.manage')

  const clusters = useQuery({
    queryKey: ['clusters'],
    queryFn: ({ signal }) => api.clusters(signal),
    refetchInterval: 30_000,
  })

  const list = clusters.data?.clusters ?? []
  const current = list.find((c) => c.id === location.clusterId) ?? null

  const openAccount = (action: AccountAction) => {
    navigate({ section: 'settings', settingsView: 'account' })
    setAccountFocus(action)
  }

  return (
    <div className="flex h-full flex-col" style={{ backgroundColor: 'var(--bg-base)' }}>
      <header
        className="flex h-11 shrink-0 items-center gap-3 border-b px-3"
        style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-subtle)' }}
      >
        <button
          type="button"
          onClick={() => navigate({ section: 'clusters', clusterId: null, objectName: null })}
          className="flex shrink-0 items-center gap-2"
          aria-label="Kubby home"
        >
          <Logo size={20} />
        </button>

        <ClusterPicker
          clusters={list}
          current={current}
          canManage={canManage}
          onSelect={(clusterId) =>
            navigate({ section: 'clusters', clusterId, namespaces: [], typeKey: 'overview', objectName: null })
          }
          onManage={() => navigate({ section: 'manage' })}
        />

        {location.section !== 'clusters' && (
          <span
            className="font-mono uppercase tracking-[0.1em]"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
          >
            {location.section === 'manage' ? 'clusters' : location.settingsView}
          </span>
        )}

        <span className="ml-auto">
          <AccountMenu me={me} onSelect={openAccount} onSignOut={onSignOut} />
        </span>
      </header>

      <div className="min-h-0 flex-1">
        {location.section === 'settings' ? (
          <SettingsArea
            me={me}
            view={location.settingsView}
            focus={accountFocus}
            onViewChange={(settingsView) => navigate({ section: 'settings', settingsView })}
          />
        ) : location.section === 'manage' ? (
          <ManageClustersScreen
            me={me}
            onOpenCluster={(clusterId) =>
              navigate({ section: 'clusters', clusterId, namespaces: [], typeKey: 'overview', objectName: null })
            }
          />
        ) : current ? (
          <ResourceExplorer
            cluster={current}
            location={location}
            onNavigate={navigate}
            canManage={canManage}
          />
        ) : clusters.isLoading ? (
          <EmptyState title="Loading clusters…" description="" />
        ) : list.length === 0 ? (
          <EmptyState
            title="No clusters yet"
            description={
              canManage
                ? 'Add a cluster to start browsing it.'
                : 'No clusters have been shared with you yet.'
            }
            {...(canManage ? { hint: 'Use the picker above → Manage clusters' } : {})}
          />
        ) : (
          <EmptyState
            title="Pick a cluster"
            description="Choose one from the picker in the top-left corner."
          />
        )}
      </div>

      <StatusBar connection={connection} version={version} detail={detail} />
    </div>
  )
}

function SettingsArea({
  me,
  view,
  focus,
  onViewChange,
}: {
  me: Me
  view: 'account' | 'users'
  focus: AccountAction | null
  onViewChange: (view: 'account' | 'users') => void
}) {
  const canManageUsers = me.permissions.includes('user.manage')

  return (
    <div className="flex h-full">
      <nav
        className="w-48 shrink-0 border-r p-1"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
        aria-label="Settings"
      >
        <PanelLink label="Account" active={view === 'account'} onClick={() => onViewChange('account')} />
        {canManageUsers && (
          <PanelLink label="Users" active={view === 'users'} onClick={() => onViewChange('users')} />
        )}
      </nav>

      <div className="min-w-0 flex-1">
        {view === 'account' && <AccountScreen me={me} focus={focus} />}
        {view === 'users' && canManageUsers && <UsersScreen me={me} />}
      </div>
    </div>
  )
}

function PanelLink({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex h-8 w-full items-center px-2.5 text-left transition-colors"
      style={{
        borderRadius: 'var(--radius-sharp)',
        fontSize: 'var(--text-secondary-size)',
        color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
        backgroundColor: active ? 'var(--bg-active)' : 'transparent',
      }}
    >
      {label}
    </button>
  )
}
