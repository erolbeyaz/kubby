import type { KeyboardEvent, ReactNode } from 'react'

import { DISPLAY_TIMEZONE, formatClock } from '@/lib/time'

/**
 * The dashboard's drawing primitives.
 *
 * Plain SVG rather than a charting library: every shape here is a ring, a bar or a filled
 * line, and a library that draws all three would cost more bytes than the whole screen.
 * It also keeps the colours on the theme's status tokens, which is what makes orange mean
 * the same thing on every panel.
 */

export const SERIES_COLOURS = [
  'var(--accent)',
  'var(--status-info)',
  'var(--status-warn)',
  'var(--env-dr)',
  'var(--status-error)',
  'var(--status-unknown)',
  'var(--status-ok)',
  '#8b7cf6',
]

/** A number large enough to read across a desk, which is the point of a stat tile. */
export function StatTile({
  label,
  value,
  sub,
  tone = 'var(--accent)',
}: {
  label: string
  value: string
  sub?: string
  tone?: string
}) {
  return (
    <div
      className="flex flex-col justify-between p-3"
      style={{
        // A wash of the tone rather than a solid fill: a wall of saturated tiles is
        // exactly as hard to scan as a wall of grey ones.
        background: `linear-gradient(140deg, color-mix(in srgb, ${tone} 26%, var(--bg-surface)), var(--bg-surface))`,
        borderLeft: `2px solid ${tone}`,
        minHeight: '5.25rem',
      }}
    >
      <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}>{label}</span>
      <span
        className="font-mono tabular-nums leading-none"
        style={{ fontSize: '2rem', color: 'var(--text-primary)' }}
      >
        {value}
      </span>
      <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>{sub ?? ' '}</span>
    </div>
  )
}

export interface Slice {
  name: string
  value: number
  colour: string
}

/**
 * A ring, with the legend beside it.
 *
 * A ring rather than a pie: the hole carries the total, which is the number people
 * actually want, and comparing angles is easier without a centre to argue about.
 */
export function Donut({
  slices,
  total,
  centreLabel,
  size = 132,
  onSelect,
  selected,
  detailFor,
}: {
  slices: Slice[]
  total?: number
  centreLabel?: string
  size?: number
  /** Makes the ring interactive: a slice and its legend row become buttons. */
  onSelect?: (name: string) => void
  selected?: string | null | undefined
  /** Rendered under the selected legend row — the things behind that slice. */
  detailFor?: (name: string) => ReactNode
}) {
  const sum = total ?? slices.reduce((acc, slice) => acc + slice.value, 0)

  if (sum <= 0) {
    return <Empty>nothing to show</Empty>
  }

  const radius = 54
  const circumference = 2 * Math.PI * radius

  // The running total is computed up front rather than accumulated while rendering: a
  // variable mutated inside the map is a value that depends on render order.
  const startAt = slices.reduce<number[]>(
    (acc, slice, index) => [...acc, (acc[index] ?? 0) + slice.value],
    [0],
  )

  return (
    <div className="flex items-center gap-4">
      <svg viewBox="0 0 140 140" width={size} height={size} className="shrink-0" role="img">
        {slices.map((slice, index) => {
          const fraction = slice.value / sum
          // Drawn as one long dash per slice, rotated into place: no arc maths, and the
          // gaps between slices come free from the dash array.
          const dash = `${Math.max(fraction * circumference - 2, 0)} ${circumference}`
          const rotation = ((startAt[index] ?? 0) / sum) * 360 - 90

          const open = selected === slice.name

          return (
            <circle
              key={slice.name}
              cx="70"
              cy="70"
              r={radius}
              fill="none"
              stroke={slice.colour}
              // The selected slice thickens rather than changing colour: colour already
              // means which slice this is, and overloading it would say two things at once.
              strokeWidth={open ? 28 : 22}
              strokeDasharray={dash}
              transform={`rotate(${rotation} 70 70)`}
              // The ring is the thing people point at. Leaving it inert and making only
              // the label beside it clickable puts the target somewhere nobody aims.
              {...(onSelect
                ? {
                    role: 'button',
                    tabIndex: 0,
                    'aria-expanded': open,
                    'aria-label': `${slice.name}: ${format(slice.value)}`,
                    style: { cursor: 'pointer' },
                    onClick: () => onSelect(open ? '' : slice.name),
                    onKeyDown: (event: KeyboardEvent<SVGCircleElement>) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault()
                        onSelect(open ? '' : slice.name)
                      }
                    },
                  }
                : {})}
            >
              <title>{`${slice.name}: ${format(slice.value)}`}</title>
            </circle>
          )
        })}

        <text
          x="70"
          y="66"
          textAnchor="middle"
          className="font-mono"
          style={{ fontSize: '22px', fill: 'var(--text-primary)' }}
        >
          {format(sum)}
        </text>
        {centreLabel && (
          <text
            x="70"
            y="84"
            textAnchor="middle"
            style={{ fontSize: '10px', fill: 'var(--text-muted)' }}
          >
            {centreLabel}
          </text>
        )}
      </svg>

      <ul className="min-w-0 flex-1">
        {slices.map((slice) => {
          const open = selected === slice.name
          const row = (
            <>
              <span
                aria-hidden="true"
                className="h-2 w-2 shrink-0"
                style={{ backgroundColor: slice.colour, borderRadius: 1 }}
              />
              <span className="min-w-0 flex-1 truncate" style={{ color: 'var(--text-secondary)' }}>
                {slice.name}
              </span>
              <span className="font-mono tabular-nums" style={{ color: 'var(--text-primary)' }}>
                {format(slice.value)}
              </span>
              <span
                className="w-10 text-right font-mono tabular-nums"
                style={{ color: 'var(--text-muted)' }}
              >
                {((slice.value / sum) * 100).toFixed(1)}%
              </span>
            </>
          )

          return (
            <li key={slice.name} style={{ fontSize: 'var(--text-micro)' }}>
              {onSelect ? (
                <button
                  type="button"
                  onClick={() => onSelect(open ? '' : slice.name)}
                  aria-expanded={open}
                  className="flex w-full items-center gap-2 py-0.5 text-left"
                  style={{ color: open ? 'var(--text-primary)' : undefined }}
                >
                  {row}
                </button>
              ) : (
                <span className="flex items-center gap-2 py-0.5">{row}</span>
              )}
              {open && detailFor && <div className="pb-1 pl-4">{detailFor(slice.name)}</div>}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/** Columns, for a handful of things compared side by side — nodes, most often. */
export function BarColumns({
  bars,
  max = 100,
  unit = '%',
  toneOf,
}: {
  bars: { name: string; value: number }[]
  max?: number
  unit?: string
  toneOf?: (value: number) => string
}) {
  if (bars.length === 0) return <Empty>no readings</Empty>

  return (
    <div className="flex items-end gap-2" style={{ height: '7.5rem' }}>
      {bars.map((bar) => {
        const height = `${Math.min((bar.value / max) * 100, 100)}%`
        const colour = toneOf?.(bar.value) ?? 'var(--accent)'

        return (
          <div key={bar.name} className="flex min-w-0 flex-1 flex-col items-center gap-1">
            <span
              className="font-mono tabular-nums"
              style={{ fontSize: '11px', color: colour }}
            >
              {Math.round(bar.value)}
              {unit}
            </span>
            <div
              className="relative w-full flex-1"
              style={{ backgroundColor: 'var(--bg-active)', borderRadius: 2 }}
            >
              <div
                className="absolute bottom-0 w-full"
                style={{
                  height,
                  borderRadius: 2,
                  background: `linear-gradient(to top, ${colour}, color-mix(in srgb, ${colour} 45%, transparent))`,
                }}
              />
            </div>
            <span
              className="w-full truncate text-center font-mono"
              style={{ fontSize: '10px', color: 'var(--text-muted)' }}
              title={bar.name}
            >
              {shortNode(bar.name)}
            </span>
          </div>
        )
      })}
    </div>
  )
}

/** Rows, for a ranked list where the names matter more than the shape. */
export function BarRows({
  bars,
  format: formatValue = format,
}: {
  bars: { name: string; value: number }[]
  format?: (value: number) => string
}) {
  if (bars.length === 0) return <Empty>no readings</Empty>

  const max = Math.max(...bars.map((bar) => bar.value), 1)

  return (
    <div className="flex flex-col gap-1">
      {bars.map((bar, index) => (
        <div key={bar.name} className="flex items-center gap-2">
          <span
            className="w-24 shrink-0 truncate font-mono"
            style={{ fontSize: '11px', color: 'var(--text-secondary)' }}
            title={bar.name}
          >
            {bar.name}
          </span>
          <span
            className="h-3 min-w-0 flex-1 overflow-hidden"
            style={{ backgroundColor: 'var(--bg-active)', borderRadius: 2 }}
          >
            <span
              className="block h-full"
              style={{
                width: `${(bar.value / max) * 100}%`,
                borderRadius: 2,
                background: `linear-gradient(90deg, ${SERIES_COLOURS[index % SERIES_COLOURS.length]}, color-mix(in srgb, ${SERIES_COLOURS[index % SERIES_COLOURS.length]} 40%, transparent))`,
              }}
            />
          </span>
          <span
            className="w-16 shrink-0 text-right font-mono tabular-nums"
            style={{ fontSize: '11px', color: 'var(--text-primary)' }}
          >
            {formatValue(bar.value)}
          </span>
        </div>
      ))}
    </div>
  )
}

export interface AreaSeries {
  name: string
  colour: string
  points: { at: string; value: number }[]
}

/**
 * Several filled lines over one time axis, with the legend as a table.
 *
 * The table is the part that earns its place: min, max and current together answer "is
 * this normal" in a way the line alone cannot, and reading them off a chart by eye is
 * exactly the work this is meant to save.
 */
export function AreaChart({
  series,
  unit = '',
  height = 150,
  render,
}: {
  series: AreaSeries[]
  unit?: string
  height?: number
  /** For a chart whose numbers are not plain counts — bytes, seconds, a rate. */
  render?: ((value: number) => string) | undefined
}) {
  const show = (value: number) => (render ? render(value) : `${format(value)}${unit}`)

  const drawn = series.filter((line) => line.points.length >= 2)
  if (drawn.length === 0) return <Empty>not enough history yet</Empty>

  const everyValue = drawn.flatMap((line) => line.points.map((point) => point.value))
  const top = Math.max(...everyValue, 1) * 1.1
  const width = 100

  // Read off the longest line: every series here comes from the same query range, and a
  // shorter one is a target that started late rather than a different window.
  const axis = drawn.reduce((longest, line) =>
    line.points.length > longest.points.length ? line : longest,
  ).points
  const ticks = axisTicks(axis)

  return (
    <div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        className="w-full"
        style={{ height }}
        role="img"
      >
        {[0.25, 0.5, 0.75].map((line) => (
          <line
            key={line}
            x1="0"
            x2={width}
            y1={height * line}
            y2={height * line}
            stroke="var(--border-subtle)"
            strokeWidth="0.4"
          />
        ))}

        {drawn.map((line) => {
          const step = width / (line.points.length - 1)
          const y = (value: number) => height - (value / top) * (height - 4) - 2
          const path = line.points
            .map((point, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(2)},${y(point.value).toFixed(2)}`)
            .join(' ')

          return (
            <g key={line.name}>
              <path d={`${path} L${width},${height} L0,${height} Z`} fill={line.colour} opacity="0.16" />
              <path
                d={path}
                fill="none"
                stroke={line.colour}
                strokeWidth="1.5"
                strokeLinejoin="round"
                vectorEffect="non-scaling-stroke"
              />
            </g>
          )
        })}
      </svg>

      {/* When. A line with no axis says "something changed" without saying when, which is
          the half that separates a rollout from an outage. Three ticks rather than a
          crowded row: the ends bound the window and the middle gives it a scale.
          Server time is UTC (ADR-026); the conversion happens here, once. */}
      <span
        className="mt-1 flex justify-between font-mono"
        style={{ fontSize: '10px', color: 'var(--text-muted)' }}
        title={`times in ${DISPLAY_TIMEZONE}`}
      >
        {ticks.map((tick, index) => (
          <span key={`${tick}-${index}`}>{tick}</span>
        ))}
      </span>

      <table className="mt-2 w-full" style={{ fontSize: '11px' }}>
        <thead>
          <tr style={{ color: 'var(--text-muted)' }}>
            <th className="text-left font-normal">series</th>
            <th className="w-14 text-right font-normal">min</th>
            <th className="w-14 text-right font-normal">max</th>
            <th className="w-14 text-right font-normal">current</th>
          </tr>
        </thead>
        <tbody>
          {drawn.map((line) => {
            const values = line.points.map((point) => point.value)
            return (
              <tr key={line.name}>
                <td className="py-0.5">
                  <span className="flex items-center gap-1.5">
                    <span
                      aria-hidden="true"
                      className="h-2 w-2 shrink-0"
                      style={{ backgroundColor: line.colour, borderRadius: 1 }}
                    />
                    <span style={{ color: 'var(--text-secondary)' }}>{line.name}</span>
                  </span>
                </td>
                <td className="text-right font-mono tabular-nums" style={{ color: 'var(--text-muted)' }}>
                  {show(Math.min(...values))}
                </td>
                <td className="text-right font-mono tabular-nums" style={{ color: 'var(--text-muted)' }}>
                  {show(Math.max(...values))}
                </td>
                <td className="text-right font-mono tabular-nums" style={{ color: 'var(--text-primary)' }}>
                  {show(values[values.length - 1] ?? 0)}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/**
 * Start, middle and end. The window a chart covers is written on it rather than only in
 * the panel's heading, because the heading is gone as soon as the reader scrolls.
 */
function axisTicks(points: Array<{ at: string }>): string[] {
  if (points.length === 0) return []

  const first = points[0]?.at ?? ''
  const last = points[points.length - 1]?.at ?? ''
  const middle = points[Math.floor((points.length - 1) / 2)]?.at ?? ''

  // A window that crosses midnight needs the date, or 22:00 and 02:00 leave the reader
  // working out which of them is yesterday.
  const spansDays =
    new Date(last).getTime() - new Date(first).getTime() > 20 * 60 * 60 * 1000

  return [formatClock(first, spansDays), formatClock(middle, spansDays), formatClock(last, spansDays)]
}

export function Empty({ children }: { children: ReactNode }) {
  return (
    <div
      className="flex items-center justify-center py-6"
      style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
    >
      {children}
    </div>
  )
}

export function format(value: number): string {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}k`
  // Whole numbers first: a count of 12 pods reading as "12.0" looks like a measurement
  // rather than a tally.
  if (Number.isInteger(value)) return String(value)
  if (value >= 10) return value.toFixed(1)
  return value.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
}

export function formatBytes(value: number): string {
  if (value >= 1 << 30) return `${(value / (1 << 30)).toFixed(1)} GiB`
  if (value >= 1 << 20) return `${Math.round(value / (1 << 20))} MiB`
  if (value >= 1 << 10) return `${Math.round(value / (1 << 10))} KiB`
  return `${Math.round(value)} B`
}

/** Node names are long and share a prefix; the tail is the part that differs. */
function shortNode(name: string): string {
  const parts = name.split('-')
  return parts.length > 2 ? parts.slice(-2).join('-') : name
}
