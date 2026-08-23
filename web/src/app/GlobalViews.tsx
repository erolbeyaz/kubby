import { Icon, type IconName } from '@/components/Icon'

export interface GlobalView {
  id: string
  label: string
  icon: IconName
  /** Views about one cluster are unreachable until one is chosen. */
  needsCluster: boolean
}

/**
 * The views that are about the whole picture rather than one kind of object.
 *
 * They live in the header beside the mark, apart from the resource tree: the tree
 * answers "which objects", these answer "how is it". Later phases add entries here.
 */
export const GLOBAL_VIEWS: GlobalView[] = [
  // Overview answers "how is everything": the fleet at the top, then this cluster. A
  // separate Fleet button asked the same question at a different scale and made the
  // reader choose between two answers to it.
  { id: 'overview', label: 'Overview', icon: 'health', needsCluster: false },
  { id: 'health', label: 'Health', icon: 'warning', needsCluster: true },
]

interface GlobalViewsProps {
  active: string
  hasCluster: boolean
  onSelect: (id: string) => void
}

export function GlobalViews({ active, hasCluster, onSelect }: GlobalViewsProps) {
  return (
    <nav aria-label="Overviews" className="flex items-center gap-0.5">
      {GLOBAL_VIEWS.map((view) => {
        const disabled = view.needsCluster && !hasCluster
        const selected = active === view.id

        return (
          <button
            key={view.id}
            type="button"
            disabled={disabled}
            onClick={() => onSelect(view.id)}
            aria-current={selected ? 'page' : undefined}
            {...(disabled ? { title: 'Choose a cluster first' } : {})}
            className={`nav-chip${selected ? ' nav-chip--active' : ''}`}
          >
            <Icon name={view.icon} />
            {view.label}
          </button>
        )
      })}
    </nav>
  )
}
