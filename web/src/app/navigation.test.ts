import { describe, expect, it } from 'vitest'

import { buildPath, parseLocation } from './navigation'

describe('navigation', () => {
  it('round-trips every navigable view through a URL', () => {
    const cases = [
      '/clusters',
      '/clusters/c1/payments/pods',
      '/clusters/c1/payments/apps/deployments',
      '/clusters/c1/-/nodes',
      '/manage',
      '/settings/account',
      '/settings/users',
    ]

    for (const path of cases) {
      expect(buildPath(parseLocation(path, ''))).toBe(path)
    }
  })

  // Everything that changes the screen has to be in the URL, or back cannot retrace it.
  it('carries the open object', () => {
    const location = parseLocation('/clusters/c1/payments/pods', '?object=ledger-0')

    expect(location.objectName).toBe('ledger-0')
    expect(buildPath(location)).toBe('/clusters/c1/payments/pods?object=ledger-0')
  })

  it('keeps grouped kinds addressable', () => {
    const location = parseLocation('/clusters/c1/payments/apps/deployments', '')

    expect(location.typeKey).toBe('apps/deployments')
    expect(location.namespaces).toEqual(['payments'])
  })

  // A dash keeps the path shape constant for cluster-wide views.
  it('uses a dash for cluster-wide views', () => {
    expect(parseLocation('/clusters/c1/-/nodes', '').namespaces).toEqual([])
    expect(buildPath({ ...parseLocation('/clusters/c1/-/nodes', '') })).toBe('/clusters/c1/-/nodes')
  })

  // A service rarely lives in exactly one namespace.
  it('carries several namespaces at once', () => {
    const location = parseLocation('/clusters/c1/payments,storefront/pods', '')

    expect(location.namespaces).toEqual(['payments', 'storefront'])
    expect(buildPath(location)).toBe('/clusters/c1/payments%2Cstorefront/pods')
  })

  it('defaults to the cluster list', () => {
    expect(parseLocation('/', '')).toMatchObject({ section: 'clusters', clusterId: null })
  })

  it('falls back to the account view for an unknown settings page', () => {
    expect(parseLocation('/settings/nonsense', '').settingsView).toBe('account')
  })

  // The list may span every namespace while the open object belongs to exactly one.
  // Losing that on reload made the object fetch 404 in a retry loop.
  it('keeps the open object namespace apart from the filter', () => {
    const location = parseLocation('/clusters/c1/-/pods', '?object=api-7f9&ns=payments')

    expect(location.namespaces).toEqual([])
    expect(location.objectName).toBe('api-7f9')
    expect(location.objectNamespace).toBe('payments')
    expect(buildPath(location)).toBe('/clusters/c1/-/pods?object=api-7f9&ns=payments')
  })
})
