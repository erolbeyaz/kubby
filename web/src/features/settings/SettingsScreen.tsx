import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { Icon, type IconName } from '@/components/Icon'
import { ApiError, api, type Me } from '@/lib/api'
import { UsersScreen } from '@/features/users/UsersScreen'

import { AuditSinkSection } from './AuditSinkSection'
import { LogAnalysisSection } from './LogAnalysisSection'
import { MetricsSection } from './MetricsSection'
import { NodeShellSection } from './NodeShellSection'
import { PodDebugSection } from './PodDebugSection'

type Tab = 'users' | 'shells' | 'metrics' | 'logs' | 'audit'

const TABS: { id: Tab; label: string; icon: IconName; hint: string }[] = [
  { id: 'users', label: 'Users', icon: 'shield', hint: 'Who may sign in, and what they may do' },
  { id: 'shells', label: 'Shells', icon: 'terminal', hint: 'Where a shell pod or debug container comes from' },
  { id: 'metrics', label: 'Metrics', icon: 'monitor', hint: 'Where measurements over time are read from' },
  { id: 'logs', label: 'Log analysis', icon: 'health', hint: 'What counts as a problem in a log line' },
  { id: 'audit', label: 'Audit shipping', icon: 'events', hint: 'Where the audit trail is copied to' },
]

/**
 * The deployment's own settings, as opposed to a cluster's.
 *
 * Kept apart from everything else because it is the only screen about Kubby rather than
 * about Kubernetes, and because everything on it is administrative: who may sign in, and
 * what this installation is allowed to reach.
 */
export function SettingsScreen({ me }: { me: Me }) {
  const [tab, setTab] = useState<Tab>('users')

  const settings = useQuery({
    queryKey: ['kubby-settings'],
    queryFn: ({ signal }) => api.settings(signal),
  })

  const error = settings.error instanceof ApiError ? settings.error : null

  return (
    <div className="flex h-full">
      <nav
        aria-label="Settings"
        className="w-60 shrink-0 border-r p-2"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        {TABS.map((entry) => (
          <button
            key={entry.id}
            type="button"
            onClick={() => setTab(entry.id)}
            aria-current={tab === entry.id ? 'page' : undefined}
            className="flex h-9 w-full items-center gap-2 px-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
            style={{
              borderRadius: 'var(--radius-sharp)',
              fontSize: '14px',
              color: tab === entry.id ? 'var(--text-primary)' : 'var(--text-secondary)',
              backgroundColor: tab === entry.id ? 'var(--accent-muted)' : undefined,
              boxShadow: tab === entry.id ? 'inset 2px 0 0 0 var(--accent)' : undefined,
            }}
          >
            <Icon name={entry.icon} />
            {entry.label}
          </button>
        ))}
      </nav>

      <div className="min-w-0 flex-1 overflow-auto">
        {error && (
          <div className="p-4">
            <Callout tone="error" title="Could not read the settings" requestId={error.requestId}>
              {error.message}
            </Callout>
          </div>
        )}

        {tab === 'users' && <UsersScreen me={me} />}

        {settings.data && tab === 'shells' && (
          <>
            <NodeShellSection value={settings.data.nodeShell} />
            <PodDebugSection value={settings.data.podDebug} />
          </>
        )}
        {settings.data && tab === 'metrics' && <MetricsSection value={settings.data.metrics} />}
        {settings.data && tab === 'logs' && <LogAnalysisSection value={settings.data.logAnalysis} />}
        {settings.data && tab === 'audit' && <AuditSinkSection value={settings.data.auditSink} />}
      </div>
    </div>
  )
}
