import { Suspense, lazy, useEffect } from 'react'

import { CopyButton } from './CopyButton'
import { YamlFallback } from './YamlFallback'

// Monaco is around a megabyte. Loading it with the application would make every screen
// pay for an editor most visits never open, so it arrives when the YAML tab does.
const load = () => import('./YamlViewer')

const YamlViewer = lazy(async () => ({ default: (await load()).YamlViewer }))

/**
 * Starts fetching the editor before it is asked for.
 *
 * Anyone who opens an object is likely to look at its YAML, so the chunk is warmed as
 * soon as the details are on screen. By the time the tab is clicked it is usually
 * already there, and the fallback never appears.
 */
export function warmYamlViewer() {
  void load().catch(() => {
    // A failed warm-up is not an error: the tab will retry on its own when opened.
  })
}

interface LazyYamlViewerProps {
  value: string
  ariaLabel?: string
}

export function LazyYamlViewer({ value, ariaLabel }: LazyYamlViewerProps) {
  useEffect(warmYamlViewer, [])

  return (
    <div className="relative h-full w-full">
      <Suspense fallback={<YamlFallback value={value} />}>
        <YamlViewer value={value} {...(ariaLabel ? { ariaLabel } : {})} />
      </Suspense>

      {/* Over the content rather than in a toolbar: the button belongs to this YAML, and
          a manifest is copied far too often to make it a trip to the panel header. */}
      <div className="absolute right-3 top-2 z-10">
        <CopyButton value={value} label="Copy YAML" />
      </div>
    </div>
  )
}
