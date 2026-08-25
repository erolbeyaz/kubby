import { describe, expect, it } from 'vitest'

import type { ResourceRow } from '@/lib/api'

import { applyChange } from './resource-stream'

const row = (name: string, status = 'Running'): ResourceRow => ({
  name,
  namespace: 'payments',
  age: '1m',
  createdAt: '2026-08-24T10:00:00Z',
  fields: { status },
})

describe('applyChange', () => {
  it('adds a row that was not there', () => {
    const after = applyChange([row('a')], { type: 'added', row: row('b') })

    expect(after.map((entry) => entry.name)).toEqual(['a', 'b'])
  })

  it('replaces a row in place rather than moving it', () => {
    const after = applyChange([row('a'), row('b'), row('c')], {
      type: 'modified',
      row: row('b', 'CrashLoopBackOff'),
    })

    expect(after.map((entry) => entry.name)).toEqual(['a', 'b', 'c'])
    expect(after[1]?.fields['status']).toBe('CrashLoopBackOff')
  })

  // A deletion carries no row, only what it was.
  it('removes a deleted row by name', () => {
    const after = applyChange([row('a'), row('b')], {
      type: 'deleted',
      namespace: 'payments',
      name: 'a',
    })

    expect(after.map((entry) => entry.name)).toEqual(['b'])
  })

  it('leaves a deletion for something it does not hold alone', () => {
    const rows = [row('a')]

    expect(applyChange(rows, { type: 'deleted', namespace: 'other', name: 'a' })).toEqual(rows)
  })

  // A reset means the list is no longer trustworthy; the caller relists rather than
  // patching across a gap it cannot see into.
  it('changes nothing on a reset', () => {
    const rows = [row('a')]

    expect(applyChange(rows, { type: 'reset', reason: 'the watch restarted' })).toEqual(rows)
  })

  it('ignores an update with no row', () => {
    const rows = [row('a')]

    expect(applyChange(rows, { type: 'modified' })).toEqual(rows)
  })
})
