import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { TextInput } from '@/components/Field'
import { NamespacePicker } from '@/components/NamespacePicker'
import { VirtualRows } from '@/components/VirtualRows'
import { ApiError, api, type Column, type ResourceRow } from '@/lib/api'
import { formatAbsolute } from '@/lib/time'

import { statusColor, typeKeyForKind } from './statusColor'

const ROW_HEIGHT = 30
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
}: ResourceTableProps) {
  const [search, setSearch] = useState('')
  const [sort, setSort] = useState('')
  const [desc, setDesc] = useState(false)

  const namespaceParam = namespaces.join(',')

  const list = useQuery({
    queryKey: ['resources', clusterId, typeKey, namespaceParam, search, sort, desc],
    queryFn: ({ signal }) =>
      api.resources(clusterId, typeKey, { namespace: namespaceParam, search, sort, desc }, signal),
    refetchInterval: 15_000,
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

  const grid = [NAME_WIDTH, showNamespace ? '10rem' : '', ...columns.map(widthFor)]
    .filter(Boolean)
    .join(' ')

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
          className="min-h-0 flex-1"
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
                  fontSize: 'var(--text-micro)',
                  color: 'var(--text-muted)',
                }}
              >
                <HeaderCell label="Name" sortKey="name" sort={sort} desc={desc} onSort={toggleSort} />
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

                {showNamespace && (
                  <LinkCell
                    value={row.namespace ?? ''}
                    onClick={() => onNavigate({ namespace: row.namespace ?? '' })}
                  />
                )}

                {columns.map((column) => (
                  <Cell key={column.key} column={column} row={row} onNavigate={onNavigate} />
                ))}
              </div>
            )}
          </VirtualRows>
        </div>
      )}
    </div>
  )
}

function Cell({
  column,
  row,
  onNavigate,
}: {
  column: Column
  row: ResourceRow
  onNavigate: (target: NavigationTarget) => void
}) {
  const value = column.key === 'age' ? row.age : (row.fields[column.key] ?? '')

  if (!value) {
    return (
      <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>—</span>
    )
  }

  if (column.status) {
    return (
      <span className="truncate" style={{ fontSize: 'var(--text-micro)', color: statusColor(value) }}>
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
        fontSize: 'var(--text-micro)',
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
    return <span style={{ fontSize: 'var(--text-micro)' }}>{value}</span>
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
      <span className="truncate" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }} title={title}>
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
      style={{ fontSize: 'var(--text-micro)' }}
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
