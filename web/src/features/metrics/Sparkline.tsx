import type { MetricPoint } from '@/lib/api'

interface SparklineProps {
  points: MetricPoint[]
  /** Fixed top of the scale. Percentages use 100 so two charts can be compared. */
  max?: number
  /** Above this the line turns; below it stays quiet. */
  warnAt?: number
  errorAt?: number
  height?: number
  /** Rendered under the chart, e.g. "%" or "/h". */
  unit?: string
  label: string
}

/**
 * A line over time, small enough to sit beside a number.
 *
 * The number is already on the screen from the cluster itself. What Prometheus adds is
 * the shape: 80% now is unremarkable, 80% climbing steadily for an hour is the thing
 * somebody needs to look at, and no single reading can tell those apart.
 */
export function Sparkline({
  points,
  max,
  warnAt,
  errorAt,
  height = 44,
  unit = '',
  label,
}: SparklineProps) {
  if (points.length < 2) {
    return (
      <div
        className="flex items-center justify-center"
        style={{ height, fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        not enough history yet
      </div>
    )
  }

  const values = points.map((point) => point.value)
  const latest = values[values.length - 1] ?? 0
  const peak = Math.max(...values)
  const top = max ?? Math.max(peak * 1.15, 1)

  const tone = toneFor(latest, warnAt, errorAt)
  const width = 100

  // A path in a 100-wide viewBox stretched to the container: the chart has no fixed
  // pixel width and the points must not care what it ends up being.
  const step = width / (points.length - 1)
  const y = (value: number) => height - Math.min(value / top, 1) * (height - 2) - 1
  const line = values.map((value, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(2)},${y(value).toFixed(2)}`).join(' ')
  const area = `${line} L${width},${height} L0,${height} Z`

  const gradientId = `spark-${label.replace(/\W/g, '')}`

  return (
    <div>
      <div className="flex items-baseline justify-between">
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>{label}</span>
        <span className="font-mono tabular-nums" style={{ fontSize: 'var(--text-body)', color: tone }}>
          {format(latest)}
          <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>{unit}</span>
        </span>
      </div>

      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        className="mt-1 w-full"
        style={{ height }}
        role="img"
        aria-label={`${label}: ${format(latest)}${unit}`}
      >
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={tone} stopOpacity="0.28" />
            <stop offset="100%" stopColor={tone} stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* Drawn before the line so a threshold never sits on top of the data. */}
        {warnAt !== undefined && warnAt < top && (
          <line
            x1="0"
            x2={width}
            y1={y(warnAt)}
            y2={y(warnAt)}
            stroke="var(--status-warn)"
            strokeWidth="0.5"
            strokeDasharray="2 2"
            opacity="0.5"
          />
        )}

        <path d={area} fill={`url(#${gradientId})`} />
        <path
          d={line}
          fill="none"
          stroke={tone}
          strokeWidth="1.4"
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
      </svg>

      <div className="flex justify-between" style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
        <span>peak {format(peak)}{unit}</span>
        <span>now</span>
      </div>
    </div>
  )
}

export function toneFor(value: number, warnAt?: number, errorAt?: number): string {
  if (errorAt !== undefined && value >= errorAt) return 'var(--status-error)'
  if (warnAt !== undefined && value >= warnAt) return 'var(--status-warn)'
  return 'var(--status-ok)'
}

function format(value: number): string {
  if (value >= 100) return String(Math.round(value))
  if (value >= 10) return value.toFixed(1)
  return value.toFixed(2).replace(/\.?0+$/, '') || '0'
}
