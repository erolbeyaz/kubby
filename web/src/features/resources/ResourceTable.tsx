import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { TextInput } from '@/components/Field'
import { NamespacePicker } from '@/components/NamespacePicker'
import { VirtualRows } from '@/components/VirtualRows'
import { ApiError, api, type Column, type ResourceRow } from '@/lib/api'
import { formatAbsolute, formatAge, isLiveAge } from '@/lib/time'
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

  const list = useQuery({
    queryKey: ['resources', clusterId, typeKey, namespaceParam, search, sort, desc],
    queryFn: ({ signal }) =>
      api.resources(clusterId, typeKey, { namespace: namespaceParam, search, sort, desc }, signal),
    refetchInterval: live ? 1_000 : 15_000,
    placeholderData: (previous) => previous,
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

  const grid = [
    '1.5rem',
    showName ? NAME_WIDTH : '',
    '1.5rem',
    showNamespace ? '10rem' : '',
    ...columns.map(widthFor),
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
                  <HeaderCell label="Name" sortKey="name" sort={sort} desc={desc} onSort={toggleSort} />
                )}
                <span aria-hidden="true" />
                {showNamespace && (
                  <HeaderCell label="Namespace" sortKey="namespace" sort={sort} desc={desc} onSort={toggleSort} />
                )}
                {columns.map((column) => (
                  <HeaderCell
                    key={column.key}
                    label={column.label}
                    sortKey={column.key}
                    sort={sort}
                    desc={desc}
                    onSort={toggleSort}
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
    return <ContainerPips value={value} />
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
function ContainerPips({ value }: { value: string }) {
  const [readyText, totalText] = value.split('/')
  const ready = Number(readyText)
  const total = Number(totalText)

  if (!Number.isFinite(ready) || !Number.isFinite(total)) {
    return <span style={{ fontSize: 'var(--text-secondary-size)' }}>{value}</span>
  }

  return (
    <span className="flex items-center gap-1" title={`${ready} of ${total} containers ready`}>
      {Array.from({ length: Math.min(total, 8) }, (_, index) => (
        <span
          key={index}
          className="h-2 w-2"
          style={{
            borderRadius: '1px',
            backgroundColor: index < ready ? 'var(--status-ok)' : 'var(--border-strong)',
          }}
        />
      ))}
      {total > 8 && (
        <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>+{total - 8}</span>
      )}
    </span>
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
}: {
  label: string
  sortKey: string
  sort: string
  desc: boolean
  onSort: (key: string) => void
}) {
  const active = sort === sortKey || (sort === '' && sortKey === 'name')

  return (
    <button
      type="button"
      role="columnheader"
      aria-sort={active ? (desc ? 'descending' : 'ascending') : 'none'}
      onClick={() => onSort(sortKey)}
      className={`truncate text-left transition-colors hover:text-[var(--text-primary)] ${
        active ? 'text-[var(--text-secondary)]' : ''
      }`}
    >
      {label}
      {active && <span aria-hidden="true">{desc ? ' ↓' : ' ↑'}</span>}
    </button>
  )
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

  const restarts = Number(row.fields['restarts'] ?? '0')
  if (Number.isFinite(restarts) && restarts > 0) {
    return `Restarted ${restarts} ${restarts === 1 ? 'time' : 'times'}`
  }

  if (row.severity) return `This ${kind} needs attention`
  return ''
}
