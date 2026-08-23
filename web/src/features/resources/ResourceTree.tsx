import { useQuery } from '@tanstack/react-query'

import { Collapsible } from '@/components/Collapsible'
import { Icon } from '@/components/Icon'
import { api, type ResourceType } from '@/lib/api'

import { CATEGORY_ICON, CATEGORY_LABEL, CATEGORY_ORDER } from './categories'

interface ResourceTreeProps {
  clusterId: string
  selectedType: string | null
  onSelectType: (typeKey: string) => void
}

/** The navigation panel: which namespace, and which kind within it. */
export function ResourceTree({ clusterId, selectedType, onSelectType }: ResourceTreeProps) {
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
        <button
          type="button"
          onClick={() => onSelectType('overview')}
          className="mb-1 flex h-8 w-full items-center gap-2 px-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
          style={{
            borderRadius: 'var(--radius-sharp)',
            fontSize: 'var(--text-secondary-size)',
            color: selectedType === 'overview' ? 'var(--text-primary)' : 'var(--text-secondary)',
            backgroundColor: selectedType === 'overview' ? 'var(--accent-muted)' : 'transparent',
            boxShadow: selectedType === 'overview' ? 'inset 2px 0 0 0 var(--accent)' : undefined,
          }}
        >
          <Icon name="health" />
          Overview
        </button>

        {CATEGORY_ORDER.map((category) => {
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
              {items.map((type) => {
                const active = type.key === selectedType
                return (
                  <button
                    key={type.key}
                    type="button"
                    onClick={() => onSelectType(type.key)}
                    className="flex h-7 w-full items-center justify-between rounded-sm pl-6 pr-2 text-left transition-colors hover:bg-[var(--bg-hover)]"
                    style={{
                      borderRadius: 'var(--radius-sharp)',
                      fontSize: 'var(--text-secondary-size)',
                      color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
                      backgroundColor: active ? 'var(--accent-muted)' : 'transparent',
                      boxShadow: active ? 'inset 2px 0 0 0 var(--accent)' : undefined,
                    }}
                  >
                    <span className="truncate">{type.kind}</span>
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
      </nav>
    </div>
  )
}
