import { describe, expect, it } from 'vitest'

import { TAB_ICONS, closeTab, openTab, tabId, type DockTab } from './dock'

const pod = (name: string, kind: 'logs' | 'describe' = 'logs') => ({
  kind,
  clusterId: 'c1',
  typeKey: 'pods',
  namespace: 'payments',
  name,
})

const withIds = (...items: ReturnType<typeof pod>[]): DockTab[] =>
  items.map((item) => ({ ...item, id: tabId(item) }))

describe('dock tabs', () => {
  // The reported bug: the dock followed whatever row was selected, so looking at a
  // second pod replaced the log you opened the first one to read.
  it('opens a second pod as a second tab', () => {
    const first = openTab([], pod('api-1'))
    const second = openTab(first.tabs, pod('api-2'))

    expect(second.tabs).toHaveLength(2)
    expect(second.activeId).toBe(tabId(pod('api-2')))
  })

  it('focuses an already open tab instead of duplicating it', () => {
    const first = openTab([], pod('api-1'))
    const again = openTab(first.tabs, pod('api-1'))

    expect(again.tabs).toHaveLength(1)
    expect(again.activeId).toBe(first.activeId)
  })

  it('keeps logs and describe of the same pod apart', () => {
    const opened = openTab(openTab([], pod('api-1')).tabs, pod('api-1', 'describe'))

    expect(opened.tabs).toHaveLength(2)
  })

  // Closing should leave the reader next to where they were, not at the far end.
  it('focuses the neighbour when the active tab closes', () => {
    const tabs = withIds(pod('a'), pod('b'), pod('c'))

    const closed = closeTab(tabs, tabId(pod('b')), tabId(pod('b')))

    expect(closed.tabs.map((tab) => tab.name)).toEqual(['a', 'c'])
    expect(closed.activeId).toBe(tabId(pod('c')))
  })

  it('leaves focus alone when a different tab closes', () => {
    const tabs = withIds(pod('a'), pod('b'))

    const closed = closeTab(tabs, tabId(pod('a')), tabId(pod('b')))

    expect(closed.activeId).toBe(tabId(pod('b')))
  })

  it('reports no focus once the last tab closes', () => {
    const tabs = withIds(pod('a'))

    expect(closeTab(tabs, tabId(pod('a')), tabId(pod('a')))).toEqual({ tabs: [], activeId: '' })
  })

  // Once several are open the names look alike — three pods from one ReplicaSet share
  // everything but a hash — so the icon is what says which tab is which.
  it('gives every kind its own glyph', () => {
    const kinds = Object.keys(TAB_ICONS)
    const glyphs = Object.values(TAB_ICONS)

    expect(kinds).toContain('logs')
    expect(kinds).toContain('describe')
    expect(new Set(glyphs).size).toBe(glyphs.length)
  })
})
