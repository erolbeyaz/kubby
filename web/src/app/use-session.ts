import { useCallback, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError, api } from '@/lib/api'

export type AppState = 'loading' | 'setup' | 'login' | 'ready' | 'unreachable'

/**
 * Resolves which of the top-level states the app is in.
 *
 * Signing out flips an explicit flag rather than relying on cache eviction to
 * propagate: clearing the query cache does not reliably re-render every observer, so
 * the workspace could stay on screen after the session was already revoked.
 */
export function useSession() {
  const queryClient = useQueryClient()
  const [signedOut, setSignedOut] = useState(false)
  const [signOutFailed, setSignOutFailed] = useState(false)

  const setup = useQuery({
    queryKey: ['setup-status'],
    queryFn: ({ signal }) => api.setupStatus(signal),
    retry: false,
    staleTime: 0,
  })

  const me = useQuery({
    queryKey: ['me'],
    queryFn: ({ signal }) => api.me(signal),
    retry: false,
    enabled: !signedOut && setup.data?.setupRequired === false,
  })

  const refresh = useCallback(() => {
    setSignedOut(false)
    setSignOutFailed(false)
    void queryClient.invalidateQueries({ queryKey: ['setup-status'] })
    void queryClient.invalidateQueries({ queryKey: ['me'] })
  }, [queryClient])

  const signOut = useCallback(() => {
    // Leave immediately rather than after the round trip: the user asked to go, and
    // waiting on the network makes the click feel unresponsive.
    setSignedOut(true)
    setSignOutFailed(false)
    queryClient.removeQueries({ queryKey: ['me'] })
    queryClient.removeQueries({ queryKey: ['sessions'] })
    queryClient.removeQueries({ queryKey: ['users'] })

    // The screen is already gone, but the server session is what actually matters, so
    // a failure here has to be visible: the user would otherwise believe they signed
    // out on a machine where their session is still live.
    void api.logout().catch(() => setSignOutFailed(true))
  }, [queryClient])

  let state: AppState = 'loading'
  if (setup.isError) {
    state = 'unreachable'
  } else if (setup.data?.setupRequired) {
    state = 'setup'
  } else if (signedOut) {
    state = 'login'
  } else if (me.data) {
    state = 'ready'
  } else if (me.isError) {
    const unauthenticated = me.error instanceof ApiError && (me.error.isUnauthenticated || me.error.isForbidden)
    state = unauthenticated ? 'login' : 'unreachable'
  }

  return { state, me: me.data, refresh, signOut, signOutFailed }
}
