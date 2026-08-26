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
  // One Overview. There were two while the newer design was being compared against the
  // original; the newer one won and took the name (ADR-117). Two chips asking the same
  // question made the reader choose between two answers to it.
  { id: 'overview2', label: 'Overview', icon: 'health', needsCluster: true },
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
