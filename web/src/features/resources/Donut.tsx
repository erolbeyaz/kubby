interface Segment {
  label: string
  value: number
  color: string
}

interface DonutProps {
  title: string
  segments: Segment[]
  /** The full circle: what the value is measured against. */
  /** What the ring is drawn against: what can actually be scheduled. */
  total: number
  allocatable?: number
  /** The machine's whole size, which the scheduler never gets all of. */
  capacity?: number
  format: (value: number) => string
}

const SIZE = 108
const STROKE = 11
const RADIUS = (SIZE - STROKE) / 2

/**
 * Usage against capacity as a ring.
 *
 * Each measure gets its own arc from the same origin rather than stacking, because
 * usage, requests and limits overlap by nature: a pod's usage is inside its request,
 * not additional to it, and stacking would imply a total that does not exist.
 */
export function Donut({ title, segments, total, allocatable, capacity, format }: DonutProps) {
  const safeTotal = total > 0 ? total : Math.max(...segments.map((s) => s.value), 1)

  return (
    <div className="flex flex-col items-center gap-3">
      <h3
        className="font-semibold uppercase tracking-[0.1em]"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        {title}
      </h3>

      <svg width={SIZE} height={SIZE} viewBox={`0 0 ${SIZE} ${SIZE}`} role="img" aria-label={title}>
        <circle
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={RADIUS}
          fill="none"
          stroke="var(--bg-raised)"
          strokeWidth={STROKE}
        />

        {segments.map((segment, index) => {
          const fraction = Math.min(1, segment.value / safeTotal)
          // Each arc sits on its own inset ring so overlapping measures stay readable.
          const inset = index * 0.9
          const radius = RADIUS - inset
          const circumference = 2 * Math.PI * radius

          return (
            <circle
              key={segment.label}
              cx={SIZE / 2}
              cy={SIZE / 2}
              r={radius}
              fill="none"
              stroke={segment.color}
              strokeWidth={STROKE - index * 3.2}
              strokeDasharray={`${fraction * circumference} ${circumference}`}
              strokeLinecap="butt"
              transform={`rotate(-90 ${SIZE / 2} ${SIZE / 2})`}
              style={{ transition: 'stroke-dasharray 400ms ease-out' }}
            >
              <title>{`${segment.label}: ${format(segment.value)}`}</title>
            </circle>
          )
        })}

        <text
          x={SIZE / 2}
          y={SIZE / 2 - 2}
          textAnchor="middle"
          style={{ fontSize: 17, fontFamily: 'var(--font-mono)', fill: 'var(--text-primary)' }}
        >
          {Math.round((segments[0]?.value ?? 0) / (safeTotal || 1) * 100)}%
        </text>
        <text
          x={SIZE / 2}
          y={SIZE / 2 + 13}
          textAnchor="middle"
          style={{ fontSize: 10, fill: 'var(--text-muted)' }}
        >
          of capacity
        </text>
      </svg>

      <dl className="w-full" style={{ fontSize: 'var(--text-micro)' }}>
        {segments.map((segment) => (
          <div key={segment.label} className="flex items-center gap-1.5 py-0.5">
            <span className="h-2 w-2 shrink-0" style={{ backgroundColor: segment.color, borderRadius: '1px' }} />
            <dt className="flex-1 truncate" style={{ color: 'var(--text-muted)' }}>
              {segment.label}
            </dt>
            <dd className="font-mono" style={{ color: 'var(--text-secondary)' }}>
              {format(segment.value)}
            </dd>
          </div>
        ))}
        {/* Allocatable is what the scheduler may actually hand out: capacity minus what
            the kubelet and the system reserve. Reading a cluster against capacity alone
            makes it look emptier than it can ever be. */}
        {allocatable !== undefined && (
          <div className="flex items-center gap-1.5 py-0.5">
            <span
              className="h-2 w-2 shrink-0"
              style={{ backgroundColor: 'var(--border-strong)', borderRadius: '1px' }}
            />
            <dt className="flex-1 truncate" style={{ color: 'var(--text-muted)' }}>
              Allocatable Capacity
            </dt>
            <dd className="font-mono" style={{ color: 'var(--text-secondary)' }}>
              {format(allocatable)}
            </dd>
          </div>
        )}

        <div className="flex items-center gap-1.5 py-0.5">
          <span className="h-2 w-2 shrink-0" style={{ backgroundColor: 'var(--bg-raised)', borderRadius: '1px' }} />
          <dt className="flex-1 truncate" style={{ color: 'var(--text-muted)' }}>
            Capacity
          </dt>
          <dd className="font-mono" style={{ color: 'var(--text-secondary)' }}>
            {format(capacity ?? total)}
          </dd>
        </div>
      </dl>
    </div>
  )
}
