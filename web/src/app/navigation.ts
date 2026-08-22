import { useCallback, useEffect, useState } from 'react'

/**
 * In-app location, backed by the browser's history.
 *
 * The workspace used component state for navigation, so the browser's back button left
 * the application entirely instead of stepping back through it. Every navigable view
 * now has a URL, which also makes a view shareable and survivable across a reload.
 */
export interface Location {
  section: string
  clusterId: string | null
  settingsView: 'account' | 'users'
}

const DEFAULT_SECTION = 'clusters'

export function parseLocation(pathname: string): Location {
  const parts = pathname.split('/').filter(Boolean)
  const section = parts[0] ?? DEFAULT_SECTION

  if (section === 'clusters') {
    return { section: 'clusters', clusterId: parts[1] ?? null, settingsView: 'account' }
  }
  if (section === 'settings') {
    const view = parts[1] === 'users' ? 'users' : 'account'
    return { section: 'settings', clusterId: null, settingsView: view }
  }
  return { section, clusterId: null, settingsView: 'account' }
}

export function buildPath(location: Location): string {
  if (location.section === 'clusters') {
    return location.clusterId ? `/clusters/${location.clusterId}` : '/clusters'
  }
  if (location.section === 'settings') {
    return `/settings/${location.settingsView}`
  }
  return `/${location.section}`
}

export function useNavigation() {
  const [location, setLocation] = useState<Location>(() => parseLocation(window.location.pathname))

  useEffect(() => {
    const onPopState = () => setLocation(parseLocation(window.location.pathname))
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const navigate = useCallback((next: Partial<Location>) => {
    setLocation((current) => {
      const merged = { ...current, ...next }
      const path = buildPath(merged)

      // Replacing instead of pushing for a no-op keeps the back stack meaningful:
      // clicking the section you are already on should not add a history entry.
      if (path !== window.location.pathname) {
        window.history.pushState(null, '', path)
      }
      return merged
    })
  }, [])

  return { location, navigate }
}
