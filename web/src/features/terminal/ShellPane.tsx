import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { api, type PodContainer } from '@/lib/api'
import { debugShellPath, podShellPath } from '@/lib/exec-stream'

import { TerminalPane } from './TerminalPane'

interface ShellPaneProps {
  clusterId: string
  namespace: string
  pod: string
}

/**
 * A shell in a pod, with the choice of which container it is in.
 *
 * A pod with a sidecar has two shells and they are not interchangeable: the mesh proxy is
 * almost never the one wanted, so the workload's own container is chosen by default and
 * the picker only appears when there is a decision to make (ADR-030).
 */
export function ShellPane({ clusterId, namespace, pod }: ShellPaneProps) {
  const containers = useQuery({
    queryKey: ['containers', clusterId, namespace, pod],
    queryFn: ({ signal }) => api.podContainers(clusterId, namespace, pod, signal),
  })

  const [chosen, setChosen] = useState('')
  // A debug container is a lasting change to the pod, so the reader asks for it once and
  // the session reopens against the new container.
  const [debugging, setDebugging] = useState(false)
  // An init container has already run and has nothing to exec into.
  const all = (containers.data?.containers ?? []).filter((container) => container.role !== 'init')

  if (containers.isPending) {
    return (
      <div
        className="flex h-full items-center justify-center"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        Reading the pod…
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      {all.length > 1 && (
        <div
          className="flex shrink-0 flex-wrap items-center gap-1.5 border-b px-3 py-1.5"
          style={{ borderColor: 'var(--border-subtle)' }}
        >
          {all.map((container) => {
            const selected =
              chosen === container.name || (chosen === '' && isDefaultTarget(all, container.name))
            return (
              <button
                key={container.name}
                type="button"
                onClick={() => setChosen(container.name)}
                className="tool-chip font-mono"
                style={{
                  borderColor: selected ? 'var(--accent)' : 'var(--border-default)',
                  color: selected ? 'var(--accent)' : 'var(--text-secondary)',
                }}
              >
                {container.name}
                {container.role === 'sidecar' ? ' · sidecar' : ''}
              </button>
            )
          })}
        </div>
      )}

      {/* Keyed on the container: switching is a new session, not a redirected one. */}
      <div className="min-h-0 flex-1" key={`${chosen}:${debugging}`}>
        <TerminalPane
          context={
            <>
              <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
                {namespace}/{pod}
              </span>
              {all.length > 1 ? ' · container ' : ''}
              {all.length > 1 ? (
                <span className="font-mono">
                  {chosen || all.find((container) => container.role === 'app')?.name || all[0]?.name}
                </span>
              ) : null}
            </>
          }
          path={
            debugging
              ? debugShellPath(clusterId, namespace, pod, chosen || undefined)
              : podShellPath(clusterId, namespace, pod, chosen || undefined)
          }
          {...(debugging ? { opening: 'Attaching a debug container…' } : {})}
          onDebug={() => setDebugging(true)}
        />
      </div>
    </div>
  )
}

/** The one the server would pick if asked for none: the workload's own, not the mesh's. */
function isDefaultTarget(containers: PodContainer[], name: string): boolean {
  return (containers.find((container) => container.role === 'app') ?? containers[0])?.name === name
}
