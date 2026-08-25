import { useEffect } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'

import type { ResourceList } from '@/lib/api'

import { applyChange, openResourceStream } from './resource-stream'

interface Options {
  clusterId: string
  typeKey: string
  namespaces: string[]
  /** Off while something the stream cannot answer for is in play. */
  enabled: boolean
  queryKey: QueryKey
}

/**
 * Keeps a cached list current from the cluster's own watch.
 *
 * The rows are written straight into the query cache rather than into state beside it, so
 * everything already reading that list — the table, a prefetch, a later mount — sees the
 * same thing. A reset invalidates instead of patching: the stream cannot say what
 * happened during a gap, and patching across one leaves rows that no longer exist.
 */
export function useResourceStream({ clusterId, typeKey, namespaces, enabled, queryKey }: Options) {
  const queryClient = useQueryClient()
  const namespaceKey = namespaces.join(',')
  const cacheKey = JSON.stringify(queryKey)

  useEffect(() => {
    if (!enabled) return

    return openResourceStream({ clusterId, typeKey, namespaces: namespaceKey.split(',').filter(Boolean) }, (change) => {
      if (change.type === 'reset') {
        void queryClient.invalidateQueries({ queryKey })
        return
      }

      queryClient.setQueryData<ResourceList>(queryKey, (current) => {
        if (!current) return current

        const rows = applyChange(current.rows, change)
        if (rows === current.rows) return current
        return { ...current, rows, total: rows.length }
      })
    })
    // queryKey is an array rebuilt each render; its serialisation is what identifies it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId, typeKey, namespaceKey, enabled, cacheKey, queryClient])
}
