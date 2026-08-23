import { useEffect, useState } from 'react'

import darkFull from '@/assets/kubby-logo-dark.png'
import lightFull from '@/assets/kubby-logo-light.png'
import mark from '@/assets/kubby-mark.svg'
import darkWordmark from '@/assets/kubby-wordmark-dark.svg'
import lightWordmark from '@/assets/kubby-wordmark-light.svg'

interface LogoProps {
  /** Height in pixels. Width follows the artwork's own proportions. */
  size?: number
  /**
   * mark — the cube alone.
   * wordmark — cube and name.
   * full — the artwork's own lockup, tagline included, for a screen with room for it.
   */
  variant?: 'mark' | 'wordmark' | 'full'
  /**
   * Set the tagline as live text under the wordmark.
   *
   * The artwork's own tagline is 60 units tall in a 560-unit canvas: at a header's scale
   * it renders as a three-pixel smudge. Typesetting it instead keeps it legible at any
   * size the header can afford.
   */
  tagline?: boolean
}

/**
 * Kubby's mark and wordmark, from the brand pack.
 *
 * The wordmark ships as PNG rather than the SVG master because the master sets its
 * lettering as live <text>: an SVG that names a font renders in whatever the viewer
 * happens to have, so the same file looks different on every machine. The cube is pure
 * geometry, so it stays vector and scales.
 */
const TAGLINE = 'Kubernetes, Simplified.'

export function Logo({ size = 40, variant = 'mark', tagline = false }: LogoProps) {
  const dark = useDarkTheme()

  if (variant === 'mark') {
    return <img src={mark} alt="Kubby" width={size} height={size} className="shrink-0" />
  }

  const full = variant === 'full'
  const source = full ? (dark ? darkFull : lightFull) : dark ? darkWordmark : lightWordmark

  const image = (
    <img
      src={source}
      alt="Kubby"
      // Height is what a layout has room for; width follows the artwork's own
      // proportions so it is never stretched.
      style={{ height: size, width: 'auto' }}
      className="shrink-0"
    />
  )

  if (!tagline) return image

  return (
    <span className="flex shrink-0 flex-col items-start gap-[0.15em]">
      {image}
      <span
        // Aligned under the name rather than the cube, the way the artwork sets it.
        style={{
          marginLeft: size * 0.62,
          fontSize: Math.max(9, Math.round(size * 0.3)),
          letterSpacing: '0.02em',
          color: 'var(--text-muted)',
          lineHeight: 1,
        }}
      >
        {TAGLINE}
      </span>
    </span>
  )
}

/**
 * Tracks the theme the way the stylesheet does: an explicit data-theme wins, and the
 * system preference decides when there is none.
 */
function useDarkTheme(): boolean {
  const [dark, setDark] = useState(readTheme)

  useEffect(() => {
    const media = window.matchMedia?.('(prefers-color-scheme: dark)')
    const update = () => setDark(readTheme())

    const observer = new MutationObserver(update)
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    media?.addEventListener('change', update)

    return () => {
      observer.disconnect()
      media?.removeEventListener('change', update)
    }
  }, [])

  return dark
}

function readTheme(): boolean {
  if (typeof document === 'undefined') return true

  const explicit = document.documentElement.getAttribute('data-theme')
  if (explicit === 'dark') return true
  if (explicit === 'light') return false
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true
}
