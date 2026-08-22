import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { ConnectionState } from '@/components/StatusBar'

/**
 * Polls the two unauthenticated endpoints that prove the browser, the API and the
 * database are wired together. From Faz 6 this is replaced by an SSE stream.
 */
export function useServerStatus() {
  const version = useQuery({
    queryKey: ['version'],
    queryFn: ({ signal }) => api.version(signal),
    staleTime: Infinity,
    retry: 1,
  })

  const readiness = useQuery({
    queryKey: ['readyz'],
    queryFn: ({ signal }) => api.readiness(signal),
    refetchInterval: 10_000,
    retry: false,
  })

  let connection: ConnectionState = 'connecting'
  let detail: string | undefined

  if (readiness.isError) {
    connection = 'offline'
    detail = 'API unreachable'
  } else if (readiness.data) {
    if (readiness.data.status === 'ok') {
      connection = 'ready'
    } else {
      connection = 'degraded'
      detail = readiness.data.detail ?? 'a dependency is unavailable'
    }
  }

  return { connection, detail, version: version.data }
}
