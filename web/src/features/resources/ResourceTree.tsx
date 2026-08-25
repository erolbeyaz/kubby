import { useQuery } from '@tanstack/react-query'

import { Collapsible } from '@/components/Collapsible'
import { Icon, type IconName } from '@/components/Icon'
import { api, type ResourceType } from '@/lib/api'

import { CATEGORY_ICON, CATEGORY_LABEL, CATEGORY_ORDER, kindLabel } from './categories'

interface ResourceTreeProps {
  /** Only an admin sees the deployment's own settings. */
  canManageSettings: boolean
  clusterId: string
  selectedType: string | null
  onSelectType: (typeKey: string) => void
}

/** The navigation panel: which namespace, and which kind within it. */
export function ResourceTree({
  clusterId,
  selectedType,
  onSelectType,
  canManageSettings,
}: ResourceTreeProps) {
  const types = useQuery({
    queryKey: ['resource-types', clusterId],
    queryFn: ({ signal }) => api.resourceTypes(clusterId, signal),
    staleTime: 5 * 60_000,
  })

  const byCategory = new Map<string, ResourceType[]>()
  for (const type of types.data?.types ?? []) {
    const bucket = byCategory.get(type.category) ?? []
    bucket.push(type)
    byCategory.set(type.category, bucket)
  }

  return (
    <div className="flex h-full flex-col">
      <nav className="flex-1 overflow-y-auto p-1" aria-label="Resource kinds">
        <TopEntry icon="health" label="Overview" typeKey="overview" selectedType={selectedType} onSelectType={onSelectType} />
        <TopEntry
          icon="clusters"
          label="Applications"
          typeKey="applications"
          selectedType={selectedType}
          onSelectType={onSelectType}
        />
        <TopEntry icon="storage" label="Nodes" typeKey="nodes" selectedType={selectedType} onSelectType={onSelectType} />

        <div className="my-1.5 h-px" style={{ backgroundColor: 'var(--border-default)' }} />

        {CATEGORY_ORDER.filter((category) => category !== 'cluster').map((category) => {
          const items = byCategory.get(category)
          if (!items || items.length === 0) return null

          const hasSelection = items.some((type) => type.key === selectedType)

          return (
            <Collapsible
              key={category}
              storageKey={`kubby.tree.${category}`}
              defaultOpen={category === 'workload' || hasSelection}
              title={
                <span className="flex items-center gap-1.5 font-semibold uppercase tracking-[0.08em]">
                  <Icon name={CATEGORY_ICON[category]} />
                  {CATEGORY_LABEL[category]}
                </span>
              }
            >
              {category === 'workload' && (
                <button
                  type="button"
                  onClick={() => onSelectType('workloads')}
                  className="flex h-8 w-full items-center rounded-sm pl-6 pr-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
                  style={{
                    borderRadius: 'var(--radius-sharp)',
                    fontSize: '14px',
                    color: selectedType === 'workloads' ? 'var(--text-primary)' : 'var(--text-secondary)',
                    backgroundColor: selectedType === 'workloads' ? 'var(--accent-muted)' : undefined,
                    boxShadow: selectedType === 'workloads' ? 'inset 2px 0 0 0 var(--accent)' : undefined,
                  }}
                >
                  Overview
                </button>
              )}

              {items.map((type) => {
                const active = type.key === selectedType
                return (
                  <button
                    key={type.key}
                    type="button"
                    onClick={() => onSelectType(type.key)}
                    className="flex h-8 w-full items-center justify-between rounded-sm pl-6 pr-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
                    style={{
                      borderRadius: 'var(--radius-sharp)',
                      fontSize: '14px',
                      color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
                      backgroundColor: active ? 'var(--accent-muted)' : undefined,
                      boxShadow: active ? 'inset 2px 0 0 0 var(--accent)' : undefined,
                    }}
                  >
                    <span className="truncate">{kindLabel(type.kind)}</span>
                    {type.cached && (
                      <span
                        title="Kept in a live cache"
                        aria-hidden="true"
                        className="ml-1 inline-block h-1 w-1 shrink-0 rounded-full"
                        style={{ backgroundColor: active ? 'var(--accent)' : 'var(--border-strong)' }}
                      />
                    )}
                  </button>
                )
              })}
            </Collapsible>
          )
        })}
        <TopEntry
          icon="clusters"
          label="Namespaces"
          typeKey="namespaces"
          selectedType={selectedType}
          onSelectType={onSelectType}
        />
        <TopEntry
          icon="events"
          label="Events"
          typeKey="events"
          selectedType={selectedType}
          onSelectType={onSelectType}
        />
      </nav>

      {/* Kubby's own settings, kept apart at the foot of the rail: everything above is
          about a cluster, and this is about the tool looking at it. */}
      {canManageSettings && (
        <div className="shrink-0 border-t p-1" style={{ borderColor: 'var(--border-default)' }}>
          <TopEntry
            icon="settings"
            label="Kubby Settings"
            typeKey="kubby-settings"
            selectedType={selectedType}
            onSelectType={onSelectType}
          />
        </div>
      )}
    </div>
  )
}

/** The views that belong to the cluster itself rather than to a category of objects. */
function TopEntry({
  icon,
  label,
  typeKey,
  selectedType,
  onSelectType,
  note,
}: {
  icon: IconName
  label: string
  typeKey: string
  selectedType: string | null
  onSelectType: (typeKey: string) => void
  /** Set when the entry is here to show the shape of the tool, not to be used yet. */
  note?: string
}) {
  const selected = selectedType === typeKey

  return (
    <button
      type="button"
      disabled={note !== undefined}
      onClick={() => onSelectType(typeKey)}
      {...(note ? { title: `Arrives in ${note}` } : {})}
      className="flex h-8 w-full items-center gap-2 px-2 text-left transition-colors hover:bg-[var(--bg-hover)] disabled:cursor-not-allowed disabled:opacity-40"
      style={{
        borderRadius: 'var(--radius-sharp)',
        fontSize: '14px',
        color: selected ? 'var(--text-primary)' : 'var(--text-secondary)',
        backgroundColor: selected ? 'var(--accent-muted)' : undefined,
        boxShadow: selected ? 'inset 2px 0 0 0 var(--accent)' : undefined,
      }}
    >
      <Icon name={icon} />
      <span className="flex-1 truncate">{label}</span>
      {note && (
        <span
          className="shrink-0 px-1 uppercase"
          style={{
            fontSize: '10px',
            letterSpacing: '0.06em',
            borderRadius: 'var(--radius-sharp)',
            backgroundColor: 'var(--bg-active)',
            color: 'var(--text-muted)',
          }}
        >
          soon
        </span>
      )}
    </button>
  )
}
