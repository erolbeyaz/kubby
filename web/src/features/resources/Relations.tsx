import { useQuery } from '@tanstack/react-query'

import { ApiError, api, type Relation } from '@/lib/api'

import type { NavigationTarget } from './ResourceTable'

interface RelationsProps {
  clusterId: string
  typeKey: string
  namespace: string
  name: string
  onNavigate: (target: NavigationTarget) => void
}

/**
 * What an object is part of, and what is part of it.
 *
 * Kubernetes records this as ownerReferences pointing up and label selectors pointing
 * down, so following it by hand means two different lookups in opposite directions. It is
 * the first question asked of a misbehaving pod — what created this, and is that the
 * thing to look at — so it belongs beside the pod rather than three screens away.
 */
export function Relations({ clusterId, typeKey, namespace, name, onNavigate }: RelationsProps) {
  const relations = useQuery({
    queryKey: ['relations', clusterId, typeKey, namespace, name],
    queryFn: ({ signal }) =>
      api.relations(clusterId, typeKey, { name, ...(namespace ? { namespace } : {}) }, signal),
    staleTime: 15_000,
  })

  const error = relations.error instanceof ApiError ? relations.error : null
  if (error) {
    return (
      <p className="px-3 py-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}>
        {error.message}
      </p>
    )
  }

  const all = relations.data?.relations ?? []
  if (all.length === 0) {
    return (
      <p className="px-3 py-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {relations.isLoading ? 'Following the chain…' : 'Nothing owns this and nothing is under it.'}
      </p>
    )
  }

  const owners = all.filter((relation) => relation.direction === 'owner')
  const below = all.filter((relation) => relation.direction !== 'owner')

  return (
    <div className="flex flex-col">
      {owners.length > 0 && <Group label="Created by" relations={owners} onNavigate={onNavigate} indent />}
      {below.length > 0 && (
        <Group
          label={below[0]?.direction === 'serves' ? 'Serves' : 'Runs'}
          relations={below}
          onNavigate={onNavigate}
        />
      )}
    </div>
  )
}

function Group({
  label,
  relations,
  onNavigate,
  indent = false,
}: {
  label: string
  relations: Relation[]
  onNavigate: (target: NavigationTarget) => void
  /** The owner chain steps rightward, so the shape shows how far up each one is. */
  indent?: boolean
}) {
  return (
    <div className="px-3 py-1.5">
      <p className="mb-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
        {label}
      </p>

      {relations.map((relation, index) => (
        <button
          key={`${relation.kind}/${relation.namespace}/${relation.name}`}
          type="button"
          disabled={!relation.typeKey}
          onClick={() =>
            onNavigate({
              typeKey: relation.typeKey,
              namespace: relation.namespace ?? '',
              objectName: relation.name,
            })
          }
          className="flex w-full items-baseline gap-1.5 py-0.5 text-left transition-colors hover:text-[var(--accent)] disabled:cursor-default"
          style={{
            fontSize: 'var(--text-micro)',
            paddingLeft: indent ? index * 12 : 0,
            color: relation.severity === 'error' ? 'var(--status-error)' : 'var(--status-info)',
          }}
        >
          <span className="shrink-0 font-mono" style={{ color: 'var(--text-muted)' }}>
            {relation.kind}
          </span>
          <span className="min-w-0 truncate">{relation.name}</span>
          {relation.detail && (
            <span className="shrink-0" style={{ color: 'var(--text-muted)' }}>
              {relation.detail}
            </span>
          )}
        </button>
      ))}
    </div>
  )
}
