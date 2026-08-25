import type { ResourceRow } from '@/lib/api'

export type ChangeType = 'added' | 'modified' | 'deleted' | 'reset'

export interface ResourceChange {
  type: ChangeType
  row?: ResourceRow
  namespace?: string
  name?: string
  reason?: string
}

export interface StreamOptions {
  clusterId: string
  typeKey: string
  namespaces: string[]
}

/**
 * Follows a kind over server-sent events.
 *
 * The browser reconnects on its own, which is most of why this is SSE rather than a
 * socket. What it cannot do is know that the gap it just bridged had changes in it, so
 * the server sends a reset and the caller lists again: patching a list across an
 * invisible gap leaves rows that quietly no longer exist.
 */
export function openResourceStream(
  options: StreamOptions,
  onChange: (change: ResourceChange) => void,
): () => void {
  // Somewhere without EventSource keeps the list on its poll rather than losing the
  // table: live updates are an improvement on the list, not a requirement of it.
  if (typeof EventSource === 'undefined') return () => undefined

  const query = new URLSearchParams()
  if (options.namespaces.length > 0) query.set('namespace', options.namespaces.join(','))

  const source = new EventSource(
    `/api/v1/clusters/${options.clusterId}/stream/${options.typeKey}?${query}`,
  )

  source.onmessage = (event) => {
    const change = parse(event.data)
    if (change) onChange(change)
  }

  // EventSource retries by itself; the caller only needs to know its list may be stale.
  source.onerror = () => onChange({ type: 'reset', reason: 'the connection dropped' })

  return () => source.close()
}

function parse(data: unknown): ResourceChange | null {
  if (typeof data !== 'string') return null

  try {
    const value: unknown = JSON.parse(data)
    if (typeof value !== 'object' || value === null || !('type' in value)) return null
    return value as ResourceChange
  } catch {
    return null
  }
}

/**
 * Applies one change to a list.
 *
 * Sorting is left alone: rows arrive in the order the cluster changed them, and
 * re-sorting on every event would make a list jump under the reader while they are
 * looking at it. The next list settles the order.
 */
export function applyChange(rows: ResourceRow[], change: ResourceChange): ResourceRow[] {
  const key = (row: ResourceRow) => `${row.namespace ?? ''}/${row.name}`

  switch (change.type) {
    case 'added':
    case 'modified': {
      if (!change.row) return rows
      const target = key(change.row)
      const at = rows.findIndex((row) => key(row) === target)

      if (at === -1) return [...rows, change.row]
      return rows.map((row, index) => (index === at ? change.row! : row))
    }
    case 'deleted': {
      const target = `${change.namespace ?? ''}/${change.name ?? ''}`
      return rows.filter((row) => key(row) !== target)
    }
    default:
      return rows
  }
}
