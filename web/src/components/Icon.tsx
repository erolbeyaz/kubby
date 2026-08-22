interface IconProps {
  name: IconName
  className?: string
}

export type IconName =
  | 'health'
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

// Inline 16px stroke icons keep the bundle self-contained and the weight consistent.
const PATHS: Record<IconName, string> = {
  health: 'M2 8h3l2-5 3 10 2-5h3',
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
  return (
    <svg
      viewBox="0 0 16 16"
      width="16"
      height="16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.25"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      {PATHS[name].split('M').filter(Boolean).map((segment, index) => (
        <path key={index} d={`M${segment}`} />
      ))}
    </svg>
  )
}
