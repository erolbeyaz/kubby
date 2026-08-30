import { describe, expect, it } from 'vitest'

import { kindLabel } from './categories'
import { actionsFor } from './actions'

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

describe('actionsFor', () => {
  // The declaration order groups actions by what they do; that is not the order a node
  // is used in. Opened, looked at, closed to new work, emptied, then edited or removed —
  // with the destructive one last, where a mis-click is least likely.
  it('puts a node\'s actions in the order the machine is worked on', () => {
    const ids = actionsFor('Node').map((action) => action.id)
    const wanted = ['node-shell', 'cordon', 'drain', 'edit', 'delete']

    expect(ids.filter((id) => wanted.includes(id))).toEqual(wanted)
  })

  it('leaves other kinds in their declared order', () => {
    const ids = actionsFor('Pod').map((action) => action.id)
    expect(ids.indexOf('logs')).toBeLessThan(ids.indexOf('shell'))
  })
})

