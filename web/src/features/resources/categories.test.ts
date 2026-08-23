import { describe, expect, it } from 'vitest'

import { kindLabel } from './categories'

describe('kindLabel', () => {
  // The API's spelling belongs in the API; a list of things reads as a list of things.
  it('spaces and pluralises a kind', () => {
    expect(kindLabel('DaemonSet')).toBe('Daemon Sets')
    expect(kindLabel('Pod')).toBe('Pods')
    expect(kindLabel('CronJob')).toBe('Cron Jobs')
  })

  it('does not pluralise what is already plural', () => {
    expect(kindLabel('Endpoints')).toBe('Endpoints')
  })

  it('handles the kinds English does not pluralise with an s', () => {
    expect(kindLabel('NetworkPolicy')).toBe('Network Policies')
    expect(kindLabel('Ingress')).toBe('Ingresses')
    expect(kindLabel('StorageClass')).toBe('Storage Classes')
  })
})
