interface LogoProps {
  size?: number
  showWordmark?: boolean
}

/**
 * Kubby's mark: a seven-spoke helm wheel, the shape Kubernetes made the shorthand for
 * a cluster, with the centre opened into a "k" notch so it reads as its own tool rather
 * than the Kubernetes logo.
 */
export function Logo({ size = 40, showWordmark = false }: LogoProps) {
  const spokes = Array.from({ length: 7 }, (_, i) => (i * 360) / 7)

  return (
    <div className="flex flex-col items-center gap-2">
      <svg
        width={size}
        height={size}
        viewBox="0 0 48 48"
        fill="none"
        role="img"
        aria-label="Kubby"
      >
        {/* Outer heptagon: the wheel rim */}
        <path
          d="M24 3.5 40.4 11.4 44.5 29.2 33.1 43.4 14.9 43.4 3.5 29.2 7.6 11.4Z"
          stroke="var(--accent)"
          strokeWidth="2.25"
          strokeLinejoin="round"
          fill="none"
        />

        {/* Spokes reaching from the hub toward each rim vertex */}
        <g stroke="var(--accent)" strokeWidth="1.75" strokeLinecap="round" opacity="0.55">
          {spokes.map((angle) => {
            const radians = ((angle - 90) * Math.PI) / 180
            return (
              <line
                key={angle}
                x1={24 + Math.cos(radians) * 9}
                y1={24 + Math.sin(radians) * 9}
                x2={24 + Math.cos(radians) * 16.5}
                y2={24 + Math.sin(radians) * 16.5}
              />
            )
          })}
        </g>

        {/* Hub, opened on the right so the negative space forms a k */}
        <circle cx="24" cy="24" r="8.5" fill="var(--bg-base)" stroke="var(--accent)" strokeWidth="2.25" />
        <path
          d="M21 19.5v9M21 24.4l4.2-4M21 24.1l4.4 4.4"
          stroke="var(--accent)"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          fill="none"
        />
      </svg>

      {showWordmark && (
        <span
          className="font-semibold tracking-[0.14em]"
          style={{ fontSize: 'var(--text-display)', color: 'var(--text-primary)' }}
        >
          KUBBY
        </span>
      )}
    </div>
  )
}
