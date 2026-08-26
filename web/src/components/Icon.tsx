interface IconProps {
  name: IconName
  className?: string | undefined
}

export type IconName = KubbyIconName | LucideIconName

type KubbyIconName =
  | 'health'
  | 'warning'
  | 'plus'
  | 'clusters'
  | 'workloads'
  | 'network'
  | 'storage'
  | 'events'
  | 'terminal'
  | 'settings'
  | 'chevron'
  | 'key'
  | 'shield'
  | 'monitor'
  | 'logout'

/**
 * Icons from Lucide (ISC), inlined rather than pulled from a CDN.
 *
 * The overview screens needed a wider vocabulary than the fifteen hand-drawn ones below,
 * and drawing thirteen more by hand would have produced thirteen slightly-wrong shapes.
 * Vendored as source because Kubby pins its dependencies (ADR-025) and a runtime CDN is a
 * third party in the request path of a tool that holds cluster credentials.
 *
 * These are drawn on Lucide's 24px grid; the ones above are on a 16px grid. The component
 * picks the right viewBox per icon rather than rescaling the path data, which is where
 * hand-conversion goes wrong.
 */
type LucideIconName =
  | 'pulse'
  | 'alert'
  | 'alertTriangle'
  | 'memory'
  | 'node'
  | 'pending'
  | 'pod'
  | 'refresh'
  | 'restart'
  | 'scrape'
  | 'stack'
  | 'unplug'
  | 'workload'

type Shape =
  | { t: 'path'; d: string }
  | { t: 'circle'; cx: string; cy: string; r: string }
  | { t: 'rect'; x: string; y: string; width: string; height: string; rx?: string; ry?: string }
  | { t: 'line'; x1: string; y1: string; x2: string; y2: string }

const LUCIDE: Record<LucideIconName, Shape[]> = {
  alert: [
    { t: 'path', d: "M10.268 21a2 2 0 0 0 3.464 0" },
    { t: 'path', d: "M22 8c0-2.3-.8-4.3-2-6" },
    { t: 'path', d: "M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326" },
    { t: 'path', d: "M4 2C2.8 3.7 2 5.7 2 8" },
  ],
  alertTriangle: [
    { t: 'path', d: "m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3" },
    { t: 'path', d: "M12 9v4" },
    { t: 'path', d: "M12 17h.01" },
  ],
  memory: [
    { t: 'path', d: "M6 19v-3" },
    { t: 'path', d: "M10 19v-3" },
    { t: 'path', d: "M14 19v-3" },
    { t: 'path', d: "M18 19v-3" },
    { t: 'path', d: "M8 11V9" },
    { t: 'path', d: "M16 11V9" },
    { t: 'path', d: "M12 11V9" },
    { t: 'path', d: "M2 15h20" },
    { t: 'path', d: "M2 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v1.1a2 2 0 0 0 0 3.837V17a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5.1a2 2 0 0 0 0-3.837Z" },
  ],
  node: [
    { t: 'rect', width: "20", height: "8", x: "2", y: "2", rx: "2", ry: "2" },
    { t: 'rect', width: "20", height: "8", x: "2", y: "14", rx: "2", ry: "2" },
    { t: 'line', x1: "6", x2: "6.01", y1: "6", y2: "6" },
    { t: 'line', x1: "6", x2: "6.01", y1: "18", y2: "18" },
  ],
  pending: [
    { t: 'path', d: "M5 22h14" },
    { t: 'path', d: "M5 2h14" },
    { t: 'path', d: "M17 22v-4.172a2 2 0 0 0-.586-1.414L12 12l-4.414 4.414A2 2 0 0 0 7 17.828V22" },
    { t: 'path', d: "M7 2v4.172a2 2 0 0 0 .586 1.414L12 12l4.414-4.414A2 2 0 0 0 17 6.172V2" },
  ],
  pod: [
    { t: 'path', d: "M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z" },
    { t: 'path', d: "m3.3 7 8.7 5 8.7-5" },
    { t: 'path', d: "M12 22V12" },
  ],
  pulse: [
    { t: 'path', d: "M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2" },
  ],
  refresh: [
    { t: 'path', d: "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" },
    { t: 'path', d: "M21 3v5h-5" },
    { t: 'path', d: "M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" },
    { t: 'path', d: "M8 16H3v5" },
  ],
  restart: [
    { t: 'path', d: "M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" },
    { t: 'path', d: "M3 3v5h5" },
  ],
  scrape: [
    { t: 'path', d: "M4.9 16.1C1 12.2 1 5.8 4.9 1.9" },
    { t: 'path', d: "M7.8 4.7a6.14 6.14 0 0 0-.8 7.5" },
    { t: 'circle', cx: "12", cy: "9", r: "2" },
    { t: 'path', d: "M16.2 4.8c2 2 2.26 5.11.8 7.47" },
    { t: 'path', d: "M19.1 1.9a9.96 9.96 0 0 1 0 14.1" },
    { t: 'path', d: "M9.5 18h5" },
    { t: 'path', d: "m8 22 4-11 4 11" },
  ],
  stack: [
    { t: 'path', d: "M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83z" },
    { t: 'path', d: "M2 12a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 12" },
    { t: 'path', d: "M2 17a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 17" },
  ],
  unplug: [
    { t: 'path', d: "m19 5 3-3" },
    { t: 'path', d: "m2 22 3-3" },
    { t: 'path', d: "M6.3 20.3a2.4 2.4 0 0 0 3.4 0L12 18l-6-6-2.3 2.3a2.4 2.4 0 0 0 0 3.4Z" },
    { t: 'path', d: "M7.5 13.5 10 11" },
    { t: 'path', d: "M10.5 16.5 13 14" },
    { t: 'path', d: "m12 6 6 6 2.3-2.3a2.4 2.4 0 0 0 0-3.4l-2.6-2.6a2.4 2.4 0 0 0-3.4 0Z" },
  ],
  workload: [
    { t: 'path', d: "M2.97 12.92A2 2 0 0 0 2 14.63v3.24a2 2 0 0 0 .97 1.71l3 1.8a2 2 0 0 0 2.06 0L12 19v-5.5l-5-3-4.03 2.42Z" },
    { t: 'path', d: "m7 16.5-4.74-2.85" },
    { t: 'path', d: "m7 16.5 5-3" },
    { t: 'path', d: "M7 16.5v5.17" },
    { t: 'path', d: "M12 13.5V19l3.97 2.38a2 2 0 0 0 2.06 0l3-1.8a2 2 0 0 0 .97-1.71v-3.24a2 2 0 0 0-.97-1.71L17 10.5l-5 3Z" },
    { t: 'path', d: "m17 16.5-5-3" },
    { t: 'path', d: "m17 16.5 4.74-2.85" },
    { t: 'path', d: "M17 16.5v5.17" },
    { t: 'path', d: "M7.97 4.42A2 2 0 0 0 7 6.13v4.37l5 3 5-3V6.13a2 2 0 0 0-.97-1.71l-3-1.8a2 2 0 0 0-2.06 0l-3 1.8Z" },
    { t: 'path', d: "M12 8 7.26 5.15" },
    { t: 'path', d: "m12 8 4.74-2.85" },
    { t: 'path', d: "M12 13.5V8" },
  ],
}

// Inline 16px stroke icons keep the bundle self-contained and the weight consistent.
const PATHS: Record<KubbyIconName, string> = {
  health: 'M2 8h3l2-5 3 10 2-5h3',
  warning: 'M8 2 1.5 13.5h13L8 2zM8 6.5v3.2M8 11.6h.01',
  plus: 'M8 3v10M3 8h10',
  clusters: 'M8 2 2 5v6l6 3 6-3V5L8 2zM2 5l6 3 6-3M8 8v6',
  workloads: 'M2 3h5v5H2zM9 3h5v5H9zM2 9h5v4H2zM9 9h5v4H9z',
  network: 'M8 2v4M8 10v4M3 8h4M9 8h4M8 8a1 1 0 100-2 1 1 0 000 2z',
  storage: 'M2 4c0-1 2.7-2 6-2s6 1 6 2-2.7 2-6 2-6-1-6-2zM2 4v8c0 1 2.7 2 6 2s6-1 6-2V4M2 8c0 1 2.7 2 6 2s6-1 6-2',
  events: 'M8 2v6l4 2M8 14A6 6 0 108 2a6 6 0 000 12z',
  terminal: 'M3 4l4 4-4 4M9 12h4',
  // A symmetric eight-tooth gear drawn on the 16px grid, centred on (8,8).
  settings:
    'M8 5.6a2.4 2.4 0 100 4.8 2.4 2.4 0 000-4.8zM8 1.4v1.8M8 12.8v1.8M14.6 8h-1.8M3.2 8H1.4' +
    'M12.67 3.33l-1.27 1.27M4.6 11.4l-1.27 1.27M12.67 12.67l-1.27-1.27M4.6 4.6L3.33 3.33',
  chevron: 'M6 4l4 4-4 4',
  key: 'M10.5 2.5a3.5 3.5 0 00-3.32 4.6L2 12.28V14h1.72l.9-.9v-1.2h1.2l.9-.9V9.8h1.2l1.18-1.18A3.5 3.5 0 1010.5 2.5zM11.6 5.1h.01',
  shield: 'M8 1.8l5 1.9v4.1c0 3-2.1 5.2-5 6.4-2.9-1.2-5-3.4-5-6.4V3.7l5-1.9zM5.9 8l1.5 1.5 2.9-3',
  monitor: 'M2.5 3h11v7h-11zM6 13h4M8 10v3',
  logout: 'M6 14H3.5A1.5 1.5 0 012 12.5v-9A1.5 1.5 0 013.5 2H6M10.5 11L14 8l-3.5-3M14 8H6',
}

export function Icon({ name, className }: IconProps) {
  const lucide = LUCIDE[name as LucideIconName]

  // Stroke width is scaled with the grid so a 24-grid icon does not draw heavier than a
  // 16-grid one beside it. The two sets have to look like one set.
  return (
    <svg
      viewBox={lucide ? '0 0 24 24' : '0 0 16 16'}
      width="16"
      height="16"
      fill="none"
      stroke="currentColor"
      strokeWidth={lucide ? 1.9 : 1.25}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      {lucide
        ? lucide.map((shape, index) => <Shape key={index} shape={shape} />)
        : PATHS[name as KubbyIconName]
            .split('M')
            .filter(Boolean)
            .map((segment, index) => <path key={index} d={`M${segment}`} />)}
    </svg>
  )
}

function Shape({ shape }: { shape: Shape }) {
  switch (shape.t) {
    case 'circle':
      return <circle cx={shape.cx} cy={shape.cy} r={shape.r} />
    case 'rect':
      return (
        <rect
          x={shape.x}
          y={shape.y}
          width={shape.width}
          height={shape.height}
          {...(shape.rx ? { rx: shape.rx } : {})}
          {...(shape.ry ? { ry: shape.ry } : {})}
        />
      )
    case 'line':
      return <line x1={shape.x1} y1={shape.y1} x2={shape.x2} y2={shape.y2} />
    default:
      return <path d={shape.d} />
  }
}
