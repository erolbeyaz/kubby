import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { TextInput } from '@/components/Field'
import { NamespacePicker } from '@/components/NamespacePicker'
import { VirtualRows } from '@/components/VirtualRows'
import { ApiError, api, type Column, type ContainerState, type ResourceRow } from '@/lib/api'

import { ActiveForwards } from './ActiveForwards'
import { formatAbsolute, formatAge, isLiveAge } from '@/lib/time'
import { useResourceStream } from '@/lib/use-resource-stream'
import { useTicker } from '@/lib/use-ticker'

import { statusColor, typeKeyForKind } from './statusColor'

// Dense, but a list that has to be squinted at is not dense, it is cramped.
const ROW_HEIGHT = 36
const NAME_WIDTH = 'minmax(14rem, 2fr)'

/** The namespace picker and the search box are one control pair, so one width. */
const FILTER_WIDTH = '14rem'

export interface NavigationTarget {
  typeKey?: string | undefined
  namespace?: string | undefined
  objectName?: string | null | undefined
}

interface ResourceTableProps {
  clusterId: string
  typeKey: string
  kind: string
  namespaces: string[]
  allNamespaces: string[]
  namespaceScoped: boolean
  onNamespacesChange: (namespaces: string[]) => void
  onOpen: (row: ResourceRow) => void
  onPrefetch: (row: ResourceRow) => void
  onNavigate: (target: NavigationTarget) => void
  selectedName?: string | null
  /** Clicking away from a row closes the detail panel. */
  onDismiss: () => void
  onContextMenu: (row: ResourceRow, at: { x: number; y: number }) => void
  selection: Set<string>
  onSelectionChange: (next: Set<string>) => void
  /** Whether this reader may delete; the buttons are absent rather than inert if not. */
  canWrite: boolean
  onCreate: () => void
  onDeleteSelected: (rows: ResourceRow[]) => void
  /**
   * Refresh every second rather than every fifteen.
   *
   * A controller replaces a deleted pod within a second. On the ordinary poll the reader
   * sees the row vanish and, fifteen seconds later, a similarly named one appear — which
   * reads as the delete having failed and then undone itself. Watching it happen is the
   * difference between a confusing non-event and an explanation.
   */
  live: boolean
}

export function ResourceTable({
  clusterId,
  typeKey,
  kind,
  namespaces,
  allNamespaces,
  namespaceScoped,
  onNamespacesChange,
  onOpen,
  onPrefetch,
  onNavigate,
  selectedName,
  onDismiss,
  onContextMenu,
  selection,
  onSelectionChange,
  canWrite,
  onCreate,
  onDeleteSelected,
  live,
}: ResourceTableProps) {
  const [search, setSearch] = useState('')
  const [sort, setSort] = useState('')
  const [desc, setDesc] = useState(false)

  const namespaceParam = namespaces.join(',')

  const queryKey = ['resources', clusterId, typeKey, namespaceParam, search, sort, desc]
  const list = useQuery({
    queryKey,
    queryFn: ({ signal }) =>
      api.resources(clusterId, typeKey, { namespace: namespaceParam, search, sort, desc }, signal),
    // The stream keeps the list current; the poll is the floor under it for anything the
    // watch cannot express — a server-side search, a sort, a cluster that refuses watches.
    refetchInterval: live ? 1_000 : 60_000,
    placeholderData: (previous) => previous,
  })

  // A search is answered by the API server, so a streamed row cannot be told whether it
  // still matches. While one is typed the list stays on the poll.
  useResourceStream({
    clusterId,
    typeKey,
    namespaces,
    enabled: search === '',
    queryKey,
  })

  const error = list.error instanceof ApiError ? list.error : null
  // controlledByKind rides along to make the owner clickable; it is not a column.
  const columns = (list.data?.columns ?? []).filter((c) => c.key !== 'controlledByKind')
  const rows = list.data?.rows ?? []
  // The column belongs to the kind, not to the filter. Dropping it whenever a single
  // namespace is picked makes the table change shape under the user and loses the one
  // field that says where a row came from once the filter is widened again.
  const showNamespace = namespaceScoped && rows.some((row) => row.namespace)
  // An Event's name is a generated suffix; the column would be a wall of hashes.
  const showName = !list.data?.hideName

  // Column widths are remembered per kind: pods and secrets hold different things, and
  // a width someone dragged to fit an image reference should not be undone by opening
  // a list of config maps.
  const { widthOf, resize, reset } = useColumnWidths(`kubby.columns.${kind}`)

  const grid = [
    '1.5rem',
    showName ? widthOf('name', NAME_WIDTH) : '',
    '1.5rem',
    '1.25rem',
    showNamespace ? widthOf('namespace', '10rem') : '',
    ...columns.map((column) => widthOf(column.key, widthFor(column))),
    '1.75rem',
  ]
    .filter(Boolean)
    .join(' ')

  // Ticking only matters while something on screen is recent enough to show seconds.
  const hasRecent = rows.some((row) => isLiveAge(row.createdAt))
  const now = useTicker(hasRecent)

  const keyOf = (row: ResourceRow) => `${row.namespace ?? ''}/${row.name}`
  const allSelected = rows.length > 0 && rows.every((row) => selection.has(keyOf(row)))
  const someSelected = rows.some((row) => selection.has(keyOf(row)))

  const toggleRow = (row: ResourceRow) => {
    const next = new Set(selection)
    const key = keyOf(row)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    onSelectionChange(next)
  }

  const toggleAll = () =>
    onSelectionChange(allSelected ? new Set() : new Set(rows.map(keyOf)))

  const toggleSort = (key: string) => {
    if (sort === key) {
      setDesc((value) => !value)
      return
    }
    setSort(key)
    setDesc(false)
  }

  return (
    <div className="flex h-full flex-col">
      {/* Namespace and filter sit together above the list they narrow, not in the
          navigation panel: they belong to what is on screen, not to getting there. */}
      <header
        className="flex h-12 shrink-0 items-center gap-2 border-b px-3"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        {/* The two read as a pair — narrow, then narrow again — so they are one size
            and one height rather than a short box beside a tall one. */}
        {namespaceScoped && (
          <div style={{ width: FILTER_WIDTH }}>
            <NamespacePicker
              namespaces={allNamespaces}
              selected={namespaces}
              onChange={onNamespacesChange}
            />
          </div>
        )}

        <div style={{ width: FILTER_WIDTH }}>
          <TextInput
            placeholder={`Search ${kind}…`}
            aria-label={`Filter ${kind}`}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ height: 32, width: '100%' }}
          />
        </div>

        <span
          className="ml-1 whitespace-nowrap font-mono"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
        >
          {list.data?.total ?? 0} items
        </span>

        {live && (
          <span
            title="Watching for the cluster to settle"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--accent)' }}
          >
            live
          </span>
        )}

        {list.data?.fromCache && (
          <span
            title="Served from a live cache"
            aria-label="cached"
            className="h-1.5 w-1.5 rounded-full"
            style={{ backgroundColor: 'var(--accent)' }}
          />
        )}

        {/* Open tunnels, where they can be stopped. On the toolbar rather than in a pane
            of their own: a forward lives in a browser tab now, and the only thing left
            to do here is end it. */}
        <span className="ml-auto">
          <ActiveForwards clusterId={clusterId} />
        </span>
      </header>

      {error && (
        <div className="p-4">
          <Callout tone="error" title={`Could not list ${kind}`} requestId={error.requestId}>
            {error.message}
          </Callout>
        </div>
      )}

      {!error && rows.length === 0 && !list.isLoading && (
        <EmptyState
          title={`No ${kind}`}
          description={
            search
              ? 'Nothing matches the filter.'
              : namespaces.length > 0
                ? `None in ${namespaces.join(', ')}.`
                : 'None in this cluster.'
          }
        />
      )}

      {rows.length > 0 && (
        <div
          className="relative min-h-0 flex-1"
          // Clicking the empty area below the rows dismisses the detail panel; a row's
          // own handler stops the event before it reaches here.
          onClick={onDismiss}
        >
          <VirtualRows
            items={rows}
            rowHeight={ROW_HEIGHT}
            header={
              <div
                role="row"
                aria-label="Columns"
                className="sticky top-0 z-10 grid items-center gap-3 border-b px-4"
                style={{
                  gridTemplateColumns: grid,
                  height: ROW_HEIGHT,
                  backgroundColor: 'var(--bg-surface)',
                  borderColor: 'var(--border-default)',
                  fontSize: 'var(--text-secondary-size)',
                  color: 'var(--text-muted)',
                }}
              >
                <input
                  type="checkbox"
                  aria-label="Select all"
                  checked={allSelected}
                  ref={(element) => {
                    // Some-but-not-all is its own state: an unticked box would say
                    // nothing is selected while rows below are.
                    if (element) element.indeterminate = someSelected && !allSelected
                  }}
                  onChange={toggleAll}
                  onClick={(event) => event.stopPropagation()}
                />
                {showName && (
                  <HeaderCell
                    label="Name"
                    sortKey="name"
                    sort={sort}
                    desc={desc}
                    onSort={toggleSort}
                    onResize={resize}
                    onReset={reset}
                  />
                )}
                <span aria-hidden="true" />
                <span aria-hidden="true" />
                {showNamespace && (
                  <HeaderCell
                    label="Namespace"
                    sortKey="namespace"
                    sort={sort}
                    desc={desc}
                    onSort={toggleSort}
                    onResize={resize}
                    onReset={reset}
                  />
                )}
                {columns.map((column) => (
                  <HeaderCell
                    key={column.key}
                    label={column.label}
                    sortKey={column.key}
                    sort={sort}
                    desc={desc}
                    onSort={toggleSort}
                    onResize={resize}
                    onReset={reset}
                  />
                ))}
                <span aria-hidden="true" />
              </div>
            }
          >
            {(row) => (
              <div
                key={`${row.namespace}/${row.name}`}
                onClick={(event) => {
                  event.stopPropagation()
                  onOpen(row)
                }}
                onContextMenu={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  onContextMenu(row, { x: event.clientX, y: event.clientY })
                }}
                onMouseEnter={() => onPrefetch(row)}
                className="grid cursor-pointer items-center gap-3 border-b px-4 transition-colors hover:bg-[var(--bg-hover)]"
                style={{
                  gridTemplateColumns: grid,
                  height: ROW_HEIGHT,
                  borderColor: 'var(--border-subtle)',
                  fontSize: 'var(--text-secondary-size)',
                  backgroundColor: row.name === selectedName ? 'var(--accent-muted)' : undefined,
                  boxShadow: row.name === selectedName ? 'inset 2px 0 0 0 var(--accent)' : undefined,
                }}
              >
                <input
                  type="checkbox"
                  aria-label={`Select ${row.name}`}
                  checked={selection.has(keyOf(row))}
                  onChange={() => toggleRow(row)}
                  onClick={(event) => event.stopPropagation()}
                />

                {showName && (
                <span
                  className="flex min-w-0 items-center gap-2 text-left"
                  style={{ color: 'var(--text-primary)' }}
                >
                  {row.severity && (
                    <span
                      aria-label={row.severity}
                      className="h-1.5 w-1.5 shrink-0 rounded-full"
                      style={{
                        backgroundColor:
                          row.severity === 'error' ? 'var(--status-error)' : 'var(--status-warn)',
                      }}
                    />
                  )}
                  <span className="truncate">{row.name}</span>
                </span>
                )}

                <WarningMark row={row} kind={kind} />
                <LogMark row={row} />

                {showNamespace && (
                  <LinkCell
                    value={row.namespace ?? ''}
                    onClick={() => onNavigate({ namespace: row.namespace ?? '' })}
                  />
                )}

                {columns.map((column) => (
                  <Cell key={column.key} column={column} row={row} now={now} onNavigate={onNavigate} />
                ))}

                <button
                  type="button"
                  aria-label={`Actions for ${row.name}`}
                  onClick={(event) => {
                    event.stopPropagation()
                    const box = event.currentTarget.getBoundingClientRect()
                    onContextMenu(row, { x: box.left, y: box.bottom })
                  }}
                  className="tool-chip"
                >
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
                    <circle cx="8" cy="3.2" r="1.3" />
                    <circle cx="8" cy="8" r="1.3" />
                    <circle cx="8" cy="12.8" r="1.3" />
                  </svg>
                </button>
              </div>
            )}
          </VirtualRows>

          {canWrite && (
            <div className="pointer-events-none absolute bottom-4 right-4 flex items-center gap-2">
              <FloatingButton
                label={
                  selection.size === 0
                    ? 'Select rows to delete'
                    : `Delete ${selection.size} selected`
                }
                disabled={selection.size === 0}
                destructive
                onClick={() => onDeleteSelected(rows.filter((row) => selection.has(keyOf(row))))}
                path="M4 8h8"
              />
              <FloatingButton label="Create resource" onClick={onCreate} path="M8 4v8M4 8h8" />
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function Cell({
  column,
  row,
  now,
  onNavigate,
}: {
  now: Date
  column: Column
  row: ResourceRow
  onNavigate: (target: NavigationTarget) => void
}) {
  // Age is recomputed here rather than taken from the server's string, so a row less
  // than ten minutes old counts up on screen instead of waiting for the next poll.
  const value =
    column.key === 'age' ? formatAge(row.createdAt, now) : (row.fields[column.key] ?? '')

  if (!value) {
    return (
      <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-muted)' }}>—</span>
    )
  }

  if (column.key === 'message' && row.fields['type'] === 'Warning') {
    return (
      <span
        className="truncate"
        style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--status-error)' }}
        title={value}
      >
        {value}
      </span>
    )
  }

  if (column.status) {
    return (
      <span className="truncate" style={{ fontSize: 'var(--text-secondary-size)', color: statusColor(value) }}>
        {value}
      </span>
    )
  }

  if (column.key === 'containers') {
    return <ContainerPips value={value} states={row.containerStates} />
  }

  // The column names one image and counts the rest; the tooltip names every container
  // beside its own, because the first container is not necessarily the application.
  if (column.key === 'image') {
    return (
      <span
        className="truncate font-mono"
        style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}
        title={row.fields['images'] || value}
      >
        {value}
      </span>
    )
  }

  if (column.link === 'owner') {
    const ownerKind = row.fields['controlledByKind'] ?? ''
    const target = typeKeyForKind(ownerKind)
    return (
      <LinkCell
        value={value}
        title={ownerKind ? `${ownerKind} ${value}` : value}
        onClick={
          target
            ? () => onNavigate({ typeKey: target, namespace: row.namespace ?? '', objectName: value })
            : undefined
        }
      />
    )
  }

  // An address outside Kubby. The value is what to show; where to go rides alongside it,
  // because a host is not a URL until a scheme and a path are decided.
  if (column.link === 'external') {
    const urls = (row.fields['hostUrls'] ?? '').split(',')
    return (
      <span className="flex min-w-0 gap-1.5">
        {value.split(',').map((host, index) => {
          const target = urls[index]?.trim()
          if (!target) {
            return (
              <span key={host} className="truncate font-mono" style={{ color: 'var(--text-secondary)' }}>
                {host}
              </span>
            )
          }
          return (
            <a
              key={host}
              href={target}
              target="_blank"
              rel="noreferrer"
              title={target}
              onClick={(event) => event.stopPropagation()}
              className="truncate font-mono transition-colors hover:underline"
              style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--status-info)' }}
            >
              {host}
            </a>
          )
        })}
      </span>
    )
  }

  if (column.link === 'node') {
    return (
      <LinkCell value={value} onClick={() => onNavigate({ typeKey: 'nodes', namespace: '', objectName: value })} />
    )
  }

  return (
    <span
      className="truncate"
      style={{
        fontSize: 'var(--text-secondary-size)',
        fontFamily: column.mono ? 'var(--font-mono)' : undefined,
        color: 'var(--text-secondary)',
      }}
      title={column.key === 'age' ? formatAbsolute(row.createdAt) : value}
    >
      {value}
    </span>
  )
}

/** Ready containers as filled marks: how many are up is a shape, not a sentence. */
/**
 * One square per container: what the pod is running now, and what already ran.
 *
 * A ready count says how many containers are wrong and never which one, which is the
 * first thing asked of a pod with a sidecar in it. Application containers come first
 * and init containers after, in the order they mattered.
 */
function ContainerPips({ value, states }: { value: string; states: ContainerState[] | undefined }) {
  if (!states || states.length === 0) return <ContainerCount value={value} />

  const shown = states.slice(0, MAX_PIPS)

  return (
    <span className="flex items-center gap-1">
      {shown.map((state, index) => (
        <ContainerPip key={`${state.name}-${index}`} state={state} />
      ))}
      {states.length > shown.length && (
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          +{states.length - shown.length}
        </span>
      )}
    </span>
  )
}

const MAX_PIPS = 8

/** The count the server sends alongside, for a row whose states did not come through. */
function ContainerCount({ value }: { value: string }) {
  const [readyText, totalText] = value.split('/')
  const ready = Number(readyText)
  const total = Number(totalText)

  if (!Number.isFinite(ready) || !Number.isFinite(total)) {
    return <span style={{ fontSize: 'var(--text-secondary-size)' }}>{value}</span>
  }

  return (
    <span className="flex items-center gap-1" title={`${ready} of ${total} containers ready`}>
      {Array.from({ length: Math.min(total, MAX_PIPS) }, (_, index) => (
        <span
          key={index}
          className="h-2 w-2"
          style={{
            borderRadius: '1px',
            backgroundColor: index < ready ? 'var(--status-ok)' : 'var(--border-strong)',
          }}
        />
      ))}
      {total > MAX_PIPS && (
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>+{total - MAX_PIPS}</span>
      )}
    </span>
  )
}

/**
 * What a container's square says.
 *
 * A dim square is one that did its job and exited — an init container that completed is
 * not a container that is missing, and colouring it like a failure sends the reader
 * looking for a problem that is not there.
 */
function pipColour(state: ContainerState): string {
  switch (state.state) {
    case 'running':
      return state.ready ? 'var(--status-ok)' : 'var(--status-warn)'
    case 'waiting':
      return 'var(--status-error)'
    case 'terminated':
      return state.exitCode === 0 ? 'var(--border-strong)' : 'var(--status-error)'
    default:
      return 'var(--border-strong)'
  }
}

function pipSummary(state: ContainerState): string {
  const readiness = state.ready ? 'ready' : 'not ready'
  const reason = state.reason ? `: ${state.reason}` : ''
  return `${state.name} — ${state.state || 'unknown'}, ${readiness}${reason}`
}

/** A square, and the container behind it while the pointer is on it. */
function ContainerPip({ state }: { state: ContainerState }) {
  const [at, setAt] = useState<{ x: number; y: number } | null>(null)

  return (
    <>
      <span
        role="img"
        aria-label={pipSummary(state)}
        className="h-2 w-2 shrink-0"
        style={{ borderRadius: '1px', backgroundColor: pipColour(state) }}
        onMouseEnter={(event) => {
          const box = event.currentTarget.getBoundingClientRect()
          setAt({ x: box.right, y: box.top })
        }}
        onMouseLeave={() => setAt(null)}
      />
      {at && <ContainerCard state={state} at={at} />}
    </>
  )
}

/**
 * The card beside the square.
 *
 * It is drawn into the document rather than into the row: the list scrolls inside a
 * transformed element, which a fixed position inside it would be measured against, and
 * the row itself clips what overflows it.
 */
function ContainerCard({ state, at }: { state: ContainerState; at: { x: number; y: number } }) {
  const rows: [string, string][] = []
  if (state.exitCode !== undefined) rows.push(['Exit Code', String(state.exitCode)])
  if (state.reason) rows.push(['Reason', state.reason])
  if (state.message) rows.push(['Message', state.message])
  if (state.startedAt) rows.push(['Started At', formatAbsolute(state.startedAt)])
  if (state.finishedAt) rows.push(['Finished At', formatAbsolute(state.finishedAt)])
  if (state.restarts) rows.push(['Restarts', String(state.restarts)])
  if (state.containerId) rows.push(['Container ID', state.containerId])

  // Flipped to the left of the square when there is no room to its right, which is
  // where this column sits on a narrow window.
  const width = 340
  const left = at.x + 10 + width > window.innerWidth ? at.x - width - 14 : at.x + 10

  return createPortal(
    <div
      role="tooltip"
      className="pointer-events-none fixed z-50 border px-2.5 py-2 shadow-lg"
      style={{
        left,
        top: Math.min(at.y - 6, window.innerHeight - 40),
        width,
        borderRadius: 'var(--radius-sharp)',
        borderColor: 'var(--border-default)',
        backgroundColor: 'var(--bg-raised)',
        fontSize: 'var(--text-micro)',
      }}
    >
      <div className="mb-1 flex items-baseline gap-2">
        <span className="min-w-0 truncate font-mono font-semibold" style={{ color: 'var(--text-primary)' }}>
          {state.name}
        </span>
        <span style={{ color: pipColour(state) }}>
          {state.state || 'unknown'}, {state.ready ? 'ready' : 'not ready'}
        </span>
        {state.init && <span style={{ color: 'var(--text-muted)' }}>init</span>}
      </div>

      {rows.map(([label, value]) => (
        <div key={label} className="flex flex-col">
          <span style={{ color: 'var(--text-muted)' }}>{label}</span>
          <span className="break-all font-mono" style={{ color: 'var(--text-secondary)' }}>
            {value}
          </span>
        </div>
      ))}
    </div>,
    document.body,
  )
}

function LinkCell({
  value,
  title,
  onClick,
}: {
  value: string
  title?: string
  onClick?: (() => void) | undefined
}) {
  if (!onClick) {
    return (
      <span className="truncate" style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }} title={title}>
        {value}
      </span>
    )
  }

  return (
    <button
      type="button"
      onClick={onClick}
      title={title ?? value}
      className="truncate text-left text-[var(--status-info)] transition-colors hover:text-[var(--accent)] hover:underline"
      style={{ fontSize: 'var(--text-secondary-size)' }}
    >
      {value}
    </button>
  )
}

function HeaderCell({
  label,
  sortKey,
  sort,
  desc,
  onSort,
  onResize,
  onReset,
}: {
  label: string
  sortKey: string
  sort: string
  desc: boolean
  onSort: (key: string) => void
  onResize: (key: string, width: number) => void
  onReset: (key: string) => void
}) {
  const active = sort === sortKey || (sort === '' && sortKey === 'name')

  return (
    <div className="relative flex min-w-0 items-center">
      <button
        type="button"
        role="columnheader"
        aria-sort={active ? (desc ? 'descending' : 'ascending') : 'none'}
        onClick={() => onSort(sortKey)}
        className={`min-w-0 flex-1 truncate text-left transition-colors hover:text-[var(--text-primary)] ${
          active ? 'text-[var(--text-secondary)]' : ''
        }`}
      >
        {label}
        {active && <span aria-hidden="true">{desc ? ' ↓' : ' ↑'}</span>}
      </button>
      <ColumnResizer columnKey={sortKey} label={label} onResize={onResize} onReset={onReset} />
    </div>
  )
}

const MIN_COLUMN_WIDTH = 48

/**
 * The grip between two columns.
 *
 * It sits in the gap the grid already leaves, so dragging it does not shift the header
 * text; the hit area is wider than the line, because a 1px target is a fussy thing to
 * catch and this one is dragged often. Double-clicking gives the column back its
 * default, which is otherwise unreachable once it has been dragged.
 */
function ColumnResizer({
  columnKey,
  label,
  onResize,
  onReset,
}: {
  columnKey: string
  label: string
  onResize: (key: string, width: number) => void
  onReset: (key: string) => void
}) {
  const [dragging, setDragging] = useState(false)
  const startX = useRef(0)
  const startWidth = useRef(0)

  useEffect(() => {
    if (!dragging) return

    const move = (event: PointerEvent) =>
      onResize(columnKey, Math.max(MIN_COLUMN_WIDTH, startWidth.current + event.clientX - startX.current))
    const up = () => setDragging(false)

    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    // A drag over the header would otherwise select its text, which makes the resize
    // feel like it broke something.
    document.body.style.userSelect = 'none'
    document.body.style.cursor = 'col-resize'

    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      document.body.style.userSelect = ''
      document.body.style.cursor = ''
    }
  }, [dragging, columnKey, onResize])

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={`Resize ${label}`}
      tabIndex={0}
      onPointerDown={(event) => {
        const cell = event.currentTarget.parentElement
        if (!cell) return
        event.preventDefault()
        event.stopPropagation()
        startX.current = event.clientX
        startWidth.current = cell.offsetWidth
        setDragging(true)
      }}
      onDoubleClick={(event) => {
        event.stopPropagation()
        onReset(columnKey)
      }}
      onKeyDown={(event) => {
        // Resizing from the keyboard, because this tool is meant to be usable without
        // a mouse. The width is read off the cell so the first press starts from what
        // is on screen rather than from a default the column may not be at.
        const cell = event.currentTarget.parentElement
        if (!cell) return
        if (event.key === 'ArrowRight') onResize(columnKey, cell.offsetWidth + 16)
        if (event.key === 'ArrowLeft') onResize(columnKey, Math.max(MIN_COLUMN_WIDTH, cell.offsetWidth - 16))
      }}
      className="absolute -right-2.5 top-0 z-10 h-full w-2 cursor-col-resize"
    >
      <div
        className="pointer-events-none mx-auto h-full w-px transition-colors"
        style={{ backgroundColor: dragging ? 'var(--accent)' : 'transparent' }}
      />
    </div>
  )
}

/**
 * Column widths the reader has dragged, remembered per kind.
 *
 * Only the columns actually dragged are stored: a default that changes later should
 * reach lists nobody has adjusted, rather than being frozen the first time someone
 * opened them.
 */
function useColumnWidths(storageKey: string) {
  const [widths, setWidths] = useState<Record<string, number>>(() => readStoredWidths(storageKey))
  const [loadedKey, setLoadedKey] = useState(storageKey)

  // The reader has moved to another kind, whose widths are its own. Reading them here
  // rather than in an effect means the first paint is already the right shape.
  if (loadedKey !== storageKey) {
    setLoadedKey(storageKey)
    setWidths(readStoredWidths(storageKey))
  }

  // A drag reads the current widths from an event handler, where state would be the
  // value captured when the handler was created.
  const widthsRef = useRef(widths)
  useEffect(() => {
    widthsRef.current = widths
  }, [widths])

  const store = useCallback(
    (next: Record<string, number>) => {
      setWidths(next)
      try {
        localStorage.setItem(storageKey, JSON.stringify(next))
      } catch {
        // Private browsing and blocked storage are fine; widths simply reset.
      }
    },
    [storageKey],
  )

  const resize = useCallback(
    (key: string, width: number) => store({ ...widthsRef.current, [key]: Math.round(width) }),
    [store],
  )

  const reset = useCallback(
    (key: string) => {
      const next = { ...widthsRef.current }
      delete next[key]
      store(next)
    },
    [store],
  )

  const widthOf = (key: string, fallback: string) =>
    widths[key] !== undefined ? `${widths[key]}px` : fallback

  return { widthOf, resize, reset }
}

function readStoredWidths(key: string): Record<string, number> {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(key) ?? '{}')
    if (stored === null || typeof stored !== 'object') return {}

    const widths: Record<string, number> = {}
    for (const [column, width] of Object.entries(stored as Record<string, unknown>)) {
      if (typeof width === 'number' && Number.isFinite(width) && width > 0) widths[column] = width
    }
    return widths
  } catch {
    return {}
  }
}

/** Column widths chosen so the name keeps the space and the rest stays compact. */
function widthFor(column: Column): string {
  switch (column.key) {
    case 'age':
    case 'restarts':
      return '4.5rem'
    case 'containers':
    case 'qos':
    case 'cpu':
    case 'memory':
    case 'ready':
      return '6rem'
    case 'controlledBy':
    case 'node':
      return 'minmax(8rem, 1fr)'
    // Image references are long and worth reading; the column is given room to show a
    // registry host and a tag rather than eliding both.
    case 'image':
      return 'minmax(12rem, 1.6fr)'
    default:
      return 'minmax(6rem, 1fr)'
  }
}

/**
 * The create and delete buttons that float over the list.
 *
 * They sit over the rows rather than in the header because they act on what is selected
 * below them, and because a list is scrolled far more often than it is acted on.
 */
function FloatingButton({
  label,
  path,
  onClick,
  disabled = false,
  destructive = false,
}: {
  label: string
  path: string
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
}) {
  return (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation()
        onClick()
      }}
      disabled={disabled}
      title={label}
      aria-label={label}
      className="pointer-events-auto flex h-10 w-10 items-center justify-center shadow-lg transition-colors disabled:cursor-not-allowed disabled:opacity-40"
      style={{
        borderRadius: '9999px',
        backgroundColor: destructive ? 'var(--status-error)' : 'var(--accent)',
        color: 'var(--text-inverse)',
      }}
    >
      <svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
        <path d={path} />
      </svg>
    </button>
  )
}

/**
 * The mark that says this object's own logs keep reporting a problem.
 *
 * A different shape from the triangle beside it, and deliberately so. The triangle is
 * what Kubernetes reports — a fact from the API. This is a reading of somebody's log
 * output against a pattern, which is a weaker claim, and drawing them identically would
 * spend the credibility of the reliable one on the inferred one.
 *
 * Shape carries where it came from; colour carries how bad it is.
 */
function LogMark({ row }: { row: ResourceRow }) {
  const [at, setAt] = useState<{ x: number; y: number } | null>(null)

  const finding = row.logFinding
  if (!finding) return <span aria-hidden="true" />

  const colour = finding.severity === 'error' ? 'var(--status-error)' : 'var(--status-warn)'
  const pods = finding.pods && finding.pods > 1 ? ` across ${finding.pods} pods` : ''

  return (
    <>
      <span
        role="img"
        aria-label={`Logs report ${finding.rule}${pods}: ${finding.sample}`}
        className="flex h-5 w-5 shrink-0 items-center justify-center"
        style={{ color: colour }}
        onMouseEnter={(event) => {
          const box = event.currentTarget.getBoundingClientRect()
          setAt({ x: box.right, y: box.top })
        }}
        onMouseLeave={() => setAt(null)}
      >
        {/* Lines on a page: what the log says, not what the cluster says. */}
        <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
          <rect x="2" y="1.5" width="12" height="13" rx="1.5" fill="currentColor" />
          <path
            d="M4.6 5h6.8M4.6 8h6.8M4.6 11h4"
            stroke="var(--bg-base)"
            strokeWidth="1.4"
            strokeLinecap="round"
          />
        </svg>
      </span>
      {at && <LogFindingCard finding={finding} at={at} />}
    </>
  )
}

/** What the logs said, beside the mark that says they said something. */
function LogFindingCard({
  finding,
  at,
}: {
  finding: NonNullable<ResourceRow['logFinding']>
  at: { x: number; y: number }
}) {
  const colour = finding.severity === 'error' ? 'var(--status-error)' : 'var(--status-warn)'

  const width = 380
  const left = at.x + 10 + width > window.innerWidth ? at.x - width - 14 : at.x + 10

  return createPortal(
    <div
      role="tooltip"
      className="pointer-events-none fixed z-50 border px-2.5 py-2 shadow-lg"
      style={{
        left,
        top: Math.min(at.y - 6, window.innerHeight - 40),
        width,
        borderRadius: 'var(--radius-sharp)',
        borderColor: 'var(--border-default)',
        backgroundColor: 'var(--bg-raised)',
        fontSize: 'var(--text-micro)',
      }}
    >
      <div className="mb-1 flex items-baseline gap-2">
        <span className="font-semibold" style={{ color: colour }}>
          {finding.rule}
        </span>
        <span style={{ color: 'var(--text-muted)' }}>{finding.class}</span>
        <span className="ml-auto shrink-0 font-mono" style={{ color: 'var(--text-secondary)' }}>
          {finding.count.toLocaleString()} lines
        </span>
      </div>

      <div className="mb-1.5" style={{ color: 'var(--text-muted)' }}>
        {finding.pods && finding.pods > 1 && (
          <span style={{ color: 'var(--text-secondary)' }}>{finding.pods} pods · </span>
        )}
        {/* How long it has been going on is the number that decides whether to stop and
            look now. The one that started this work had been failing for 22 hours. */}
        first seen {formatAge(finding.firstSeen)} ago · last {formatAge(finding.lastSeen)} ago
      </div>

      {finding.summary && (
        <div className="mb-1.5 font-mono" style={{ color: 'var(--text-primary)' }}>
          {finding.summary}
        </div>
      )}

      <div
        className="border-t pt-1.5 font-mono break-words"
        style={{ borderColor: 'var(--border-subtle)', color: 'var(--text-secondary)' }}
      >
        {finding.sample}
      </div>
    </div>,
    document.body,
  )
}

/**
 * The triangle that says a row is not doing what it was asked to.
 *
 * Ahead of the name rather than in the status column, because it is read by scanning
 * down the left edge — the status word is what you look at once the triangle has told
 * you where to look.
 */
function WarningMark({ row, kind }: { row: ResourceRow; kind: string }) {
  const trouble = troubleWith(row, kind)
  if (!trouble) return <span aria-hidden="true" />

  return (
    <span
      role="img"
      aria-label={trouble}
      title={trouble}
      className="flex h-5 w-5 shrink-0 items-center justify-center"
      // Filled rather than outlined, and the mark cut out of it: an outline at this size
      // is a shape you have to look for, and this one has to be found by scanning.
      style={{ color: row.severity === 'error' ? 'var(--status-error)' : 'var(--status-warn)' }}
    >
      <svg width="17" height="17" viewBox="0 0 16 16" aria-hidden="true">
        <path
          d="M7.13 1.86a1 1 0 0 1 1.74 0l6.0 10.4a1 1 0 0 1-.87 1.5H1.99a1 1 0 0 1-.87-1.5z"
          fill="currentColor"
        />
        <path
          d="M8 5.4v3.5M8 11.05h.01"
          stroke="var(--bg-base)"
          strokeWidth="1.7"
          strokeLinecap="round"
          fill="none"
        />
      </svg>
    </span>
  )
}

/** States a kind considers finished or fine, and so worth no mark. */
const SETTLED = new Set(['Running', 'Completed', 'Succeeded', 'Active', 'Bound', 'Ready', 'Available'])

/**
 * What is wrong with a row, in words, or nothing.
 *
 * The server reads the object and says why — the image could not be pulled, no node had
 * room, the kernel took the memory back — and this shows that sentence. "Pending" and
 * "Failed" are the question; a tooltip that repeats them makes the reader open the object
 * to learn what the row already knew.
 */
function troubleWith(row: ResourceRow, kind: string): string {
  const detail = row.fields['trouble'] ?? ''
  const reason = row.fields['reason'] ?? ''
  const container = row.fields['troubleContainer'] ?? ''

  if (detail) {
    const head = container ? `${reason} · ${container}` : reason
    return head ? `${head}\n${detail}` : detail
  }

  // Kinds the server has no dedicated reading for still say whether they are settled.
  const status = row.fields['status'] ?? ''
  if (status && !SETTLED.has(status)) {
    return reason && reason !== status ? `${kind} is ${status}: ${reason}` : `${kind} is ${status}`
  }

  // Past restarts do not raise a mark of their own. What restarted is in the restart
  // column and why is in the panel; a settled pod that once crashed is working now.
  if (row.severity) return `This ${kind} needs attention`
  return ''
}
