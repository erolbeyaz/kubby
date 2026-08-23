import { useCallback, useEffect, useState } from 'react'

/**
 * In-app location, backed by the browser's history.
 *
 * Everything that changes what is on screen lives in the URL: which cluster, which
 * namespace, which kind, which object. That is what makes back step through the
 * application instead of leaving it, and what makes a view shareable and reloadable.
 */
export interface Location {
  /** clusters | manage | settings */
  section: string
  clusterId: string | null
  /** Empty means every namespace; several are allowed. */
  namespaces: string[]
  typeKey: string
  objectName: string | null
  /**
   * The namespace the open object lives in. It is held apart from `namespaces` because
   * the filter above the list may span several namespaces, or none at all, while the
   * object still belongs to exactly one — and a reload has only the URL to go on.
   */
  objectNamespace: string
  settingsView: 'account' | 'users'
}

const DEFAULT_TYPE = 'overview'

export const EMPTY_LOCATION: Location = {
  section: 'clusters',
  clusterId: null,
  namespaces: [],
  typeKey: DEFAULT_TYPE,
  objectName: null,
  objectNamespace: '',
  settingsView: 'account',
}

/**
 * Paths:
 *   /clusters
 *   /clusters/{id}/{namespace}/{group}/{resource}[?object=name]
 *   /manage
 *   /settings/{account|users}
 *
 * A namespace of "-" means cluster-wide, which keeps the path shape constant instead of
 * making the segment optional.
 */
export function parseLocation(pathname: string, search: string): Location {
  const parts = pathname.split('/').filter(Boolean)
  const section = parts[0] ?? 'clusters'
  const params = new URLSearchParams(search)
  const objectName = params.get('object')
  const objectNamespace = params.get('ns') ?? ''

  if (section === 'settings') {
    return {
      ...EMPTY_LOCATION,
      section: 'settings',
      settingsView: parts[1] === 'users' ? 'users' : 'account',
    }
  }
  if (section === 'manage') {
    return { ...EMPTY_LOCATION, section: 'manage' }
  }
  if (section !== 'clusters') {
    return { ...EMPTY_LOCATION, section }
  }

  const clusterId = parts[1] ?? null
  if (!clusterId) return { ...EMPTY_LOCATION }

  // Several namespaces are joined with a comma; "-" means every namespace, which keeps
  // the path shape constant rather than making the segment optional.
  const namespaces =
    parts[2] && parts[2] !== '-'
      ? decodeURIComponent(parts[2]).split(',').filter(Boolean)
      : []
  // A grouped kind occupies two segments ("apps/deployments"); a core kind occupies one.
  const typeParts = parts.slice(3)
  const typeKey = typeParts.length > 0 ? typeParts.join('/') : DEFAULT_TYPE

  return {
    section: 'clusters',
    clusterId,
    namespaces,
    typeKey,
    objectName: objectName || null,
    objectNamespace: objectName ? objectNamespace : '',
    settingsView: 'account',
  }
}

export function buildPath(location: Location): string {
  if (location.section === 'settings') return `/settings/${location.settingsView}`
  if (location.section === 'manage') return '/manage'
  if (location.section !== 'clusters') return `/${location.section}`
  if (!location.clusterId) return '/clusters'

  const namespaces =
    location.namespaces.length > 0 ? encodeURIComponent(location.namespaces.join(',')) : '-'
  const path = `/clusters/${location.clusterId}/${namespaces}/${location.typeKey}`

  if (!location.objectName) return path

  const query = new URLSearchParams({ object: location.objectName })
  if (location.objectNamespace) query.set('ns', location.objectNamespace)
  return `${path}?${query.toString()}`
}

export function useNavigation() {
  const [location, setLocation] = useState<Location>(() =>
    parseLocation(window.location.pathname, window.location.search),
  )

  useEffect(() => {
    const onPopState = () =>
      setLocation(parseLocation(window.location.pathname, window.location.search))

    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const navigate = useCallback((next: Partial<Location>) => {
    setLocation((current) => {
      const merged = { ...current, ...next }
      const path = buildPath(merged)
      const currentPath = window.location.pathname + window.location.search

      // Navigating to where you already are should not add a history entry, or back
      // would need pressing several times to show any change.
      if (path !== currentPath) {
        window.history.pushState(null, '', path)
      }
      return merged
    })
  }, [])

  return { location, navigate }
}
