import type { ReactNode } from 'react'

/**
 * What can be done to an object.
 *
 * One record, three surfaces (ADR-053): the icon strip in the detail panel, the row's
 * context menu, and keyboard shortcuts. A later phase adds records here rather than
 * building another menu.
 *
 * A record appearing is not permission. Every request is still checked on the server —
 * SelfSubjectAccessReview, the cluster's read-only lock, RBAC — because hiding a control
 * in a browser is decoration, not authorisation.
 */
export interface ResourceAction {
  id: string
  label: string
  /** Which kinds it applies to. Empty means every kind. */
  kinds: string[]
  /** Opens this dock tab. Actions that mutate arrive in phase 6 and will not use this. */
  dockTab?: 'logs' | 'describe'
  shortcut?: string
  /** Deleting and evicting are red and sit apart from the rest. */
  destructive?: boolean
  /** Unset once the action works. Until then the entry is shown but cannot be chosen. */
  comingIn?: string
  icon: ReactNode
}

const stroke = {
  fill: 'none' as const,
  stroke: 'currentColor',
  strokeWidth: 1.4,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

function glyph(path: string) {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" aria-hidden="true" {...stroke}>
      <path d={path} />
    </svg>
  )
}

const SCALABLE = ['Deployment', 'StatefulSet', 'ReplicaSet', 'ReplicationController']
const RESTARTABLE = ['Deployment', 'StatefulSet', 'DaemonSet']

export const ACTIONS: ResourceAction[] = [
  {
    id: 'details',
    label: 'Show details',
    kinds: [],
    shortcut: 'Enter',
    icon: glyph('M2.5 3.5h11M2.5 8h11M2.5 12.5h7'),
  },
  { id: 'logs', label: 'Logs', kinds: ['Pod'], dockTab: 'logs', shortcut: 'L', icon: glyph('M3 3.5h10M3 7h10M3 10.5h6') },
  {
    id: 'describe',
    label: 'Describe',
    kinds: [],
    dockTab: 'describe',
    shortcut: 'D',
    icon: glyph('M3.5 2.5h9v11h-9zM5.5 6h5M5.5 8.5h5M5.5 11h3'),
  },
  { id: 'shell', label: 'Shell', kinds: ['Pod'], comingIn: 'phase 8', icon: glyph('M3 4l3.5 4L3 12M8.5 12h4.5') },
  {
    id: 'forward',
    label: 'Port forward',
    kinds: ['Service', 'Pod'],
    comingIn: 'phase 8',
    icon: glyph('M2 8h8M7 5l3 3-3 3M12 3v10'),
  },
  { id: 'edit', label: 'Edit', kinds: [], comingIn: 'phase 6', icon: glyph('M11 2.5l2.5 2.5-8 8H3v-2.5z') },
  { id: 'scale', label: 'Scale', kinds: SCALABLE, comingIn: 'phase 6', icon: glyph('M3 9.5V13h3.5M12.5 6.5V3H9M3 13l4-4M13 3l-4 4') },
  { id: 'restart', label: 'Restart', kinds: RESTARTABLE, comingIn: 'phase 6', icon: glyph('M13 8a5 5 0 11-1.6-3.7M13 2v3h-3') },
  { id: 'trigger', label: 'Trigger', kinds: ['CronJob'], comingIn: 'phase 6', icon: glyph('M5 3.5l7 4.5-7 4.5z') },
  { id: 'suspend', label: 'Suspend', kinds: ['CronJob'], comingIn: 'phase 6', icon: glyph('M6 4v8M10 4v8') },
  { id: 'cordon', label: 'Cordon', kinds: ['Node'], comingIn: 'phase 6', icon: glyph('M8 2v12M2 8h12') },
  { id: 'drain', label: 'Drain', kinds: ['Node'], comingIn: 'phase 6', destructive: true, icon: glyph('M3 4h10l-1.5 9h-7z M6.5 7v3M9.5 7v3') },
  { id: 'evict', label: 'Evict', kinds: ['Pod'], comingIn: 'phase 6', destructive: true, icon: glyph('M6 8h7M10 5l3 3-3 3M7 3H3v10h4') },
  {
    id: 'delete',
    label: 'Delete',
    kinds: [],
    comingIn: 'phase 6',
    destructive: true,
    icon: glyph('M3 4.5h10M6 4.5V3h4v1.5M4.5 4.5l.7 9h5.6l.7-9M6.5 7v4M9.5 7v4'),
  },
]

/**
 * Config that holds nothing but data gets a short menu: there is no log to read and
 * nothing to scale, and offering either would be a control that never does anything.
 */
const DATA_ONLY = ['Secret', 'ConfigMap']
const DATA_ONLY_ACTIONS = ['details', 'describe', 'edit', 'delete']

export function actionsFor(kind: string): ResourceAction[] {
  const applicable = ACTIONS.filter(
    (action) => action.kinds.length === 0 || action.kinds.includes(kind),
  )
  if (!DATA_ONLY.includes(kind)) return applicable
  return applicable.filter((action) => DATA_ONLY_ACTIONS.includes(action.id))
}

/** The subset that works today, for the panel's icon strip. */
export function availableActionsFor(kind: string): ResourceAction[] {
  return actionsFor(kind).filter((action) => !action.comingIn && action.dockTab)
}
