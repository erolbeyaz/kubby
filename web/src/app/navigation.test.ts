import { describe, expect, it } from 'vitest'

import { buildPath, parseLocation } from './navigation'

describe('navigation', () => {
  it('round-trips every navigable view through a URL', () => {
    const cases = [
      '/clusters',
      '/clusters/8f14e45f-ceea-467a-9dd6-1b8f0b2d3c4e',
      '/settings/account',
      '/settings/users',
      '/health',
    ]

    for (const path of cases) {
      expect(buildPath(parseLocation(path))).toBe(path)
    }
  })

  it('defaults to the cluster list', () => {
    expect(parseLocation('/')).toMatchObject({ section: 'clusters', clusterId: null })
    expect(parseLocation('')).toMatchObject({ section: 'clusters' })
  })

  it('reads the open cluster from the path so a reload keeps it open', () => {
    expect(parseLocation('/clusters/abc-123').clusterId).toBe('abc-123')
    expect(parseLocation('/clusters').clusterId).toBeNull()
  })

  it('falls back to the account view for an unknown settings page', () => {
    expect(parseLocation('/settings/nonsense').settingsView).toBe('account')
    expect(parseLocation('/settings/users').settingsView).toBe('users')
  })
})
