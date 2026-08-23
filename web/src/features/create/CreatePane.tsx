import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { LazyYamlViewer } from '@/components/LazyYamlViewer'
import { ApiError, api, type ApplyResult } from '@/lib/api'
import { toYaml } from '@/lib/yaml'

import { IconButton } from '@/features/logs/ToolbarButton'

import { DiffView } from './DiffView'
import { groupedTemplates, renderTemplate } from './templates'

interface CreatePaneProps {
  clusterId: string
  namespaces: string[]
  /** Called once something was written, so the list can watch what the cluster does. */
  onChanged: () => void
  /**
   * An object to load and edit rather than a blank sheet.
   *
   * Editing and creating are the same act with a different starting point — write a
   * manifest, see what the server says it would do, apply it — so they are one panel
   * rather than two that drift apart.
   */
  editing?: { typeKey: string; namespace: string; name: string }
}

/**
 * Writing a manifest and applying it.
 *
 * Nothing reaches the cluster without a dry run first: the panel shows the change the
 * server says it would make, and only then offers to make it (ADR-011).
 */
export function CreatePane({ clusterId, namespaces, onChanged, editing }: CreatePaneProps) {
  const queryClient = useQueryClient()
  const [manifest, setManifest] = useState('')
  const [loaded, setLoaded] = useState<string | null>(null)

  // The object being edited, fetched once and turned into the YAML the reader edits.
  const object = useQuery({
    queryKey: ['object', clusterId, editing?.typeKey, editing?.namespace ?? '', editing?.name],
    queryFn: ({ signal }) =>
      api.resourceObject(
        clusterId,
        editing?.typeKey ?? '',
        { name: editing?.name ?? '', ...(editing?.namespace ? { namespace: editing.namespace } : {}) },
        signal,
      ),
    enabled: editing !== undefined,
  })

  // Deriving the editor's starting text from the fetch rather than pushing it in with an
  // effect: the manifest is state the reader owns from the first keystroke on.
  const fetched = object.data ? toYaml(object.data) : null
  if (fetched !== null && loaded === null) {
    setLoaded(fetched)
    setManifest(fetched)
  }
  const [result, setResult] = useState<ApplyResult | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [findSignal, setFindSignal] = useState(0)

  // A cluster without Gateway API should not be offered an HTTPRoute it cannot accept
  // (ADR-046).
  const types = useQuery({
    queryKey: ['resource-types', clusterId],
    queryFn: ({ signal }) => api.resourceTypes(clusterId, signal),
    staleTime: 5 * 60_000,
  })
  const available = types.data ? new Set(types.data.types.map((type) => type.kind)) : null

  const run = async (dryRun: boolean) => {
    setBusy(true)
    setError('')
    try {
      const applied = await api.apply(clusterId, { manifest, dryRun })
      setResult(applied)
      if (!dryRun) {
        void queryClient.invalidateQueries({ queryKey: ['resources'] })
        onChanged()
      }
    } catch (caught) {
      setResult(null)
      setError(caught instanceof ApiError ? caught.message : 'The manifest could not be applied.')
    } finally {
      setBusy(false)
    }
  }

  const checked = result?.dryRun === true
  const rejected = (result?.results ?? []).some((entry) => entry.error)

  return (
    <div className="flex h-full flex-col" style={{ backgroundColor: 'var(--bg-base)' }}>
      <header
        className="flex h-9 shrink-0 items-center gap-1 border-b px-2"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        {/* What you do to the manifest sits with the manifest, on the left; what you
            start from is a choice made once, so it sits out of the way on the right. */}
        <IconButton
          label="Dry run — ask the server what this would change"
          disabled={!manifest.trim() || busy}
          onClick={() => void run(true)}
          path="M4 2h5l3.2 3.2V14H4zM9 2v3.4h3.2M6.2 9.6 7.7 11.1 10.4 8"
        />

        <IconButton
          label={checked ? 'Apply the checked manifest' : 'Run a dry run first'}
          disabled={!checked || rejected || busy}
          accent
          onClick={() => void run(false)}
          path="M3 2.6h8.2l2.2 2.2v8.6H3zM5.4 2.6v3.8h5.2V2.6M5.4 13.4V9.2h5.2v4.2"
        />

        <IconButton
          label="Find in this manifest"
          disabled={!manifest}
          onClick={() => setFindSignal((count) => count + 1)}
          path="M7.2 2.4a4.8 4.8 0 100 9.6 4.8 4.8 0 000-9.6M10.8 10.8 14 14"
        />

        {busy && (
          <span className="ml-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            Working…
          </span>
        )}

        <span className="ml-auto">
          {!editing && (
          <select
            aria-label="Select template"
            value=""
            onChange={(event) => {
              const template = groupedTemplates(available)
                .flatMap((group) => group.items)
                .find((entry) => entry.kind === event.target.value)
              if (!template) return

              setManifest(renderTemplate(template, namespaces))
              setResult(null)
              setError('')
            }}
            className="h-7 w-56 border px-1.5 outline-none focus:border-[var(--accent)]"
            style={{
              fontSize: 'var(--text-micro)',
              backgroundColor: 'var(--bg-base)',
              borderColor: 'var(--border-default)',
              borderRadius: 'var(--radius-sharp)',
              color: 'var(--text-primary)',
            }}
          >
            <option value="">Select template…</option>
            {groupedTemplates(available).map(({ group, items }) =>
              group ? (
                <optgroup key={group} label={group}>
                  {items.map((template) => (
                    <option key={template.kind} value={template.kind}>
                      {template.kind}
                    </option>
                  ))}
                </optgroup>
              ) : (
                items.map((template) => (
                  <option key={template.kind} value={template.kind}>
                    {template.kind}
                  </option>
                ))
              ),
            )}
          </select>
          )}
        </span>
      </header>

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1">
          <LazyYamlViewer
            value={manifest}
            onChange={(next: string) => {
              setManifest(next)
              // The diff describes the manifest that produced it; editing invalidates it.
              setResult(null)
            }}
            ariaLabel="Manifest"
            findSignal={findSignal}
          />
        </div>

        {(result || error) && (
          <div
            className="w-1/2 min-w-0 overflow-auto border-l"
            style={{ borderColor: 'var(--border-default)' }}
          >
            {error && (
              <div className="p-3">
                <Callout tone="error" title="Could not apply">
                  {error}
                </Callout>
              </div>
            )}
            {result && <DiffView result={result} />}
          </div>
        )}
      </div>
    </div>
  )
}
