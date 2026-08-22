import { useState } from 'react'

import { AccountMenu, type AccountAction } from '@/components/AccountMenu'
import { EmptyState } from '@/components/EmptyState'
import { Logo } from '@/components/Logo'
import { IconRail, type RailItem } from '@/components/IconRail'
import { SecondaryPanel } from '@/components/SecondaryPanel'
import { StatusBar } from '@/components/StatusBar'
import { TabBar, type WorkspaceTab } from '@/components/TabBar'
import { AccountScreen } from '@/features/account/AccountScreen'
import { UsersScreen } from '@/features/users/UsersScreen'
import type { Me } from '@/lib/api'

import { useServerStatus } from './use-server-status'

const RAIL_ITEMS: readonly RailItem[] = [
  { id: 'health', label: 'Health', icon: 'health' },
  { id: 'clusters', label: 'Clusters', icon: 'clusters' },
  { id: 'workloads', label: 'Workloads', icon: 'workloads' },
  { id: 'network', label: 'Network', icon: 'network' },
  { id: 'storage', label: 'Storage', icon: 'storage' },
  { id: 'events', label: 'Events', icon: 'events' },
  { id: 'terminal', label: 'Terminal', icon: 'terminal' },
  { id: 'settings', label: 'Settings', icon: 'settings' },
]

const PANEL_TITLE: Record<string, string> = {
  health: 'Health',
  clusters: 'Clusters',
  workloads: 'Workloads',
  network: 'Network',
  storage: 'Storage',
  events: 'Events',
  terminal: 'Sessions',
  settings: 'Settings',
}

type SettingsView = 'account' | 'users'

interface ShellProps {
  me: Me
  onSignOut: () => void
}

export function Shell({ me, onSignOut }: ShellProps) {
  const [activeSection, setActiveSection] = useState('health')
  const [settingsView, setSettingsView] = useState<SettingsView>('account')
  const [accountFocus, setAccountFocus] = useState<AccountAction | null>(null)
  const { connection, detail, version } = useServerStatus()

  const canManageUsers = me.permissions.includes('user.manage')

  // The account menu jumps straight to the relevant section rather than dropping the
  // user on a settings page they then have to navigate.
  const openAccount = (action: AccountAction) => {
    setActiveSection('settings')
    setSettingsView('account')
    setAccountFocus(action)
  }
  const tabs: readonly WorkspaceTab[] =
    activeSection === 'settings'
      ? [{ id: settingsView, label: settingsView === 'account' ? 'Account' : 'Users' }]
      : [{ id: 'welcome', label: 'Welcome' }]

  return (
    <div className="flex h-full flex-col" style={{ backgroundColor: 'var(--bg-base)' }}>
      <div className="flex min-h-0 flex-1">
        <IconRail items={RAIL_ITEMS} activeId={activeSection} onSelect={setActiveSection} />

        <SecondaryPanel title={PANEL_TITLE[activeSection] ?? 'Kubby'}>
          {activeSection === 'settings' ? (
            <nav className="flex flex-col p-1">
              <PanelLink
                label="Account"
                active={settingsView === 'account'}
                onClick={() => setSettingsView('account')}
              />
              {canManageUsers && (
                <PanelLink
                  label="Users"
                  active={settingsView === 'users'}
                  onClick={() => setSettingsView('users')}
                />
              )}
            </nav>
          ) : (
            <p className="px-3 py-2 text-[13px]" style={{ color: 'var(--text-muted)' }}>
              No clusters configured yet.
            </p>
          )}

        </SecondaryPanel>

        <main className="flex min-w-0 flex-1 flex-col">
          <div
            className="flex h-11 shrink-0 items-center justify-between gap-3 border-b px-3"
            style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-subtle)' }}
          >
            <div className="flex items-center gap-2">
              <Logo size={22} />
              <span
                className="font-semibold tracking-[0.14em]"
                style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}
              >
                KUBBY
              </span>
            </div>
            <AccountMenu me={me} onSelect={openAccount} onSignOut={onSignOut} />
          </div>

          <TabBar tabs={tabs} activeId={tabs[0]?.id ?? ''} onSelect={() => undefined} />
          <div className="min-h-0 flex-1">
            {activeSection === 'settings' && settingsView === 'account' && (
              <AccountScreen me={me} focus={accountFocus} />
            )}
            {activeSection === 'settings' && settingsView === 'users' && canManageUsers && <UsersScreen me={me} />}
            {activeSection !== 'settings' && (
              <EmptyState
                title="No clusters yet"
                description="Cluster management arrives in the next phase. Authentication, roles and the audit trail are in place."
                hint="Phase 2 — identity and access"
              />
            )}
          </div>
        </main>
      </div>

      <StatusBar connection={connection} version={version} detail={detail} />
    </div>
  )
}

function PanelLink({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex h-8 items-center px-2.5 text-left text-[13px] transition-colors"
      style={{
        borderRadius: 'var(--radius-sharp)',
        color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
        backgroundColor: active ? 'var(--bg-active)' : 'transparent',
      }}
    >
      {label}
    </button>
  )
}
