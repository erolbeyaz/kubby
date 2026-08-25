import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { GlobalViews } from '@/app/GlobalViews'
import { AccountMenu, type AccountAction } from '@/components/AccountMenu'
import { EmptyState } from '@/components/EmptyState'
import { Icon } from '@/components/Icon'
import { Logo } from '@/components/Logo'
import { StatusBar } from '@/components/StatusBar'
import { CommandPalette } from '@/features/search/CommandPalette'
import { AccountScreen } from '@/features/account/AccountScreen'
import { ManageClustersScreen } from '@/features/clusters/ManageClustersScreen'
import { FleetHealth } from '@/features/health/FleetHealth'
import { ResourceExplorer } from '@/features/resources/ResourceExplorer'
import { SettingsScreen } from '@/features/settings/SettingsScreen'
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
  // The explorer draws its own rail and carries the status strip inside it.
  const inExplorer = location.section === 'clusters' && current !== null

  const [searching, setSearching] = useState(false)

  // Ctrl+K anywhere, except while typing into something: the shortcut belongs to the
  // window, and stealing it from a YAML editor or a terminal would be worse than not
  // having it.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.key !== 'k') return

      const target = event.target as HTMLElement | null
      const typing =
        target?.tagName === 'INPUT' ||
        target?.tagName === 'TEXTAREA' ||
        target?.isContentEditable === true
      if (typing) return

      event.preventDefault()
      setSearching(true)
    }

    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])

  const openClusterOverview = (clusterId: string) =>
    navigate({
      section: 'clusters',
      clusterId,
      namespaces: [],
      typeKey: 'overview',
      objectName: null,
      objectNamespace: '',
    })

  const openAccount = (action: AccountAction) => {
    navigate({ section: 'settings', settingsView: 'account' })
    setAccountFocus(action)
  }

  return (
    <div className="flex h-full flex-col" style={{ backgroundColor: 'var(--bg-base)' }}>
      <header
        className="flex h-12 shrink-0 items-center gap-3 border-b px-3"
        style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-subtle)' }}
      >
        {/* The mark goes to the current cluster's overview, which is where someone
            clicking "home" in a cluster tool means to end up. */}
        <button
          type="button"
          onClick={() =>
            navigate(
              location.clusterId
                ? { section: 'clusters', typeKey: 'overview', objectName: null, objectNamespace: '' }
                : { section: 'clusters', clusterId: null, objectName: null, objectNamespace: '' },
            )
          }
          className="flex shrink-0 items-center"
          aria-label="Kubby home"
        >
          <Logo size={28} variant="wordmark" />
        </button>

        <span className="h-5 w-px shrink-0" style={{ backgroundColor: 'var(--border-default)' }} />

        <GlobalViews
          active={location.section === 'clusters' ? (location.clusterId ? location.typeKey : 'overview') : ''}
          hasCluster={current !== null}
          onSelect={(view) =>
            navigate(
              current
                ? { section: 'clusters', typeKey: view, objectName: null, objectNamespace: '' }
                : { section: 'clusters', clusterId: null, objectName: null, objectNamespace: '' },
            )
          }
        />

        {location.section !== 'clusters' && (
          <span
            className="font-mono uppercase tracking-[0.1em]"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
          >
            {location.section === 'manage' ? 'clusters' : location.settingsView}
          </span>
        )}

        <span className="ml-auto flex items-center gap-2">
          {canManage && (
            <button
              type="button"
              onClick={() => navigate({ section: 'manage' })}
              className="nav-chip"
              title="Add and manage clusters"
            >
              <Icon name="plus" />
              Clusters
            </button>
          )}
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
            clusters={list}
            location={location}
            onNavigate={navigate}
            canManage={canManage}
            onSelectCluster={(clusterId) =>
              navigate({
                section: 'clusters',
                clusterId,
                namespaces: [],
                typeKey: 'overview',
                objectName: null,
                objectNamespace: '',
              })
            }
            onManageClusters={() => navigate({ section: 'manage' })}
            footer={(leading) => (
              <StatusBar connection={connection} version={version} detail={detail} leading={leading} />
            )}
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
          // With no cluster chosen, the landing screen answers the question people
          // actually arrive with: which of my clusters is broken (ADR-056).
          <FleetHealth onOpen={openClusterOverview} />
        )}
      </div>

      {/* Only where there is no rail of its own. Inside the cluster explorer the strip
          sits in the right-hand column instead, so the rail reaches the bottom. */}
      {!inExplorer && <StatusBar connection={connection} version={version} detail={detail} />}

      {searching && (
        <CommandPalette
          onClose={() => setSearching(false)}
          onOpen={(hit) => {
            setSearching(false)
            navigate({
              section: 'clusters',
              clusterId: hit.clusterId,
              typeKey: hit.typeKey,
              namespaces: hit.namespace ? [hit.namespace] : [],
              objectName: hit.name,
              objectNamespace: hit.namespace ?? '',
            })
          }}
        />
      )}
    </div>
  )
}

/**
 * The two settings areas, which answer different questions.
 *
 * Account is about the person signed in. Kubby Settings is about the installation —
 * who may sign in at all, and what this deployment is allowed to reach — so it is
 * admin-only and lives on its own.
 */
function SettingsArea({
  me,
  view,
  focus,
  onViewChange,
}: {
  me: Me
  view: 'account' | 'users' | 'kubby'
  focus: AccountAction | null
  onViewChange: (view: 'account' | 'kubby') => void
}) {
  const canManageSettings = me.permissions.includes('settings.write')
  // "users" was its own view before the deployment settings existed; it is a section of
  // them now, and an old link should land somewhere sensible rather than nowhere.
  const active = view === 'users' ? 'kubby' : view

  return (
    <div className="flex h-full">
      <nav
        className="w-48 shrink-0 border-r p-1"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
        aria-label="Settings"
      >
        <PanelLink label="Account" active={active === 'account'} onClick={() => onViewChange('account')} />
        {canManageSettings && (
          <PanelLink
            label="Kubby Settings"
            active={active === 'kubby'}
            onClick={() => onViewChange('kubby')}
          />
        )}
      </nav>

      <div className="min-w-0 flex-1">
        {active === 'account' && <AccountScreen me={me} focus={focus} />}
        {active === 'kubby' && canManageSettings && <SettingsScreen me={me} />}
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
