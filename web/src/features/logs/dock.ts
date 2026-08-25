/** One thing the dock is showing, pinned to the object it was opened for. */
export interface DockTab {
  id: string
  kind: 'logs' | 'describe' | 'create' | 'edit' | 'shell' | 'node-shell' | 'forward' | 'terminal'
  clusterId: string
  typeKey: string
  namespace: string
  name: string
}

export function tabId(tab: Omit<DockTab, 'id'>): string {
  return `${tab.kind}:${tab.clusterId}:${tab.typeKey}:${tab.namespace}:${tab.name}`
}

const VERBS: Record<DockTab['kind'], string> = {
  logs: 'Logs',
  describe: 'Describe',
  create: 'Create',
  edit: 'Edit',
  shell: 'Shell',
  'node-shell': 'Node shell',
  forward: 'Forward',
  terminal: 'Terminal',
}

export function tabLabel(tab: DockTab): string {
  if (tab.kind === 'create') return tab.name ? `Create ${tab.name}` : 'Create resource'
  // A terminal is named for the cluster it points at, not for an object: it is not
  // about one, and "Terminal shop-web" would say it was.
  if (tab.kind === 'terminal') return 'Terminal'
  return `${VERBS[tab.kind]} ${tab.name}`
}

/**
 * A glyph per kind, so a strip of tabs is scannable by shape.
 *
 * Once several are open the names look alike — three pods from the same ReplicaSet share
 * everything but a hash — and the icon is the only part that says what each tab is for.
 * Later phases add their kinds here rather than to each tab strip.
 */
export const TAB_ICONS: Record<DockTab['kind'], string> = {
  logs: 'M2 2.4h12M2 8h12M2 13.6h7',
  describe: 'M3 2.4h10v11.2H3zM5.4 5.4h5.2M5.4 8h5.2M5.4 10.6h3.4',
  create: 'M2.4 8h11.2M8 2.4v11.2',
  edit: 'M11 1.8l3.2 3.2-9 9H2v-3.2z',
  shell: 'M2.6 3.4l4.2 4.6-4.2 4.6M8.4 12.6h5',
  'node-shell': 'M2 3.4h12v9.2H2zM4.4 6.2l2.2 1.8-2.2 1.8M8.6 9.8h3.2',
  forward: 'M2 8h8.4M7.4 4.6L10.8 8l-3.4 3.4M13 3.2v9.6',
  terminal: 'M1.8 2.6h12.4v10.8H1.8zM4.2 6l2.4 2-2.4 2M8.4 10h3.4',
}

/**
 * Opens a create tab.
 *
 * Several can be open at once and they are told apart by an index rather than by an
 * object, because a manifest that has not been applied yet is not about anything.
 */
export function openCreateTab(
  state: { tabs: DockTab[]; activeId: string },
  clusterId: string,
  typeKey: string,
): { tabs: DockTab[]; activeId: string } {
  const existing = state.tabs.filter((tab) => tab.kind === 'create').length
  const name = existing === 0 ? '' : String(existing + 1)

  return openTab(state.tabs, { kind: 'create', clusterId, typeKey, namespace: '', name })
}

/**
 * Adds a tab, or focuses the one already open for that object.
 *
 * The dock belongs to the reader, not to whatever row is selected. Opening the log of a
 * second pod is a second tab, so the first is still there to compare against — which is
 * most of what reading two pods' logs is for.
 */
export function openTab(tabs: DockTab[], next: Omit<DockTab, 'id'>): { tabs: DockTab[]; activeId: string } {
  const id = tabId(next)
  if (tabs.some((tab) => tab.id === id)) return { tabs, activeId: id }
  return { tabs: [...tabs, { ...next, id }], activeId: id }
}

/** Closes one tab and says which should take focus. */
export function closeTab(tabs: DockTab[], id: string, activeId: string): { tabs: DockTab[]; activeId: string } {
  const at = tabs.findIndex((tab) => tab.id === id)
  const remaining = tabs.filter((tab) => tab.id !== id)

  if (activeId !== id || remaining.length === 0) {
    return { tabs: remaining, activeId: remaining.length > 0 ? activeId : '' }
  }
  // Focus the neighbour rather than jumping to the far end: closing a tab should leave
  // the reader next to where they were.
  const neighbour = remaining[Math.min(at, remaining.length - 1)]
  return { tabs: remaining, activeId: neighbour?.id ?? '' }
}
