import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ResourceRow } from '@/lib/api'

import { ResourceTable } from './ResourceTable'

const COLUMNS = [
  { key: 'containers', label: 'Containers', mono: true },
  { key: 'ready', label: 'Ready', mono: true },
  { key: 'status', label: 'Status', status: true },
  { key: 'restarts', label: 'Restarts', mono: true },
  { key: 'age', label: 'Age', mono: true },
]

function pod(name: string, extra: Record<string, unknown> = {}) {
  return {
    name,
    namespace: 'payments',
    age: '2d',
    createdAt: '2026-08-21T10:00:00Z',
    fields: { ready: '1/1', status: 'Running', restarts: '0', node: 'node-a' },
    ...extra,
  }
}

/** Captures the query string the table asks for, so server-side filtering is provable. */
function mockList(rows: unknown[]) {
  const requests: string[] = []

  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      requests.push(url)

      const search = new URL(url, 'http://localhost').searchParams.get('search') ?? ''
      const filtered = search
        ? rows.filter((row) => (row as { name: string }).name.includes(search))
        : rows

      return Promise.resolve(
        new Response(
          JSON.stringify({ columns: COLUMNS, rows: filtered, total: filtered.length, fromCache: true }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )
    }),
  )
  return requests
}

interface Overrides {
  selection?: Set<string>
  onSelectionChange?: (next: Set<string>) => void
  onDeleteSelected?: (rows: ResourceRow[]) => void
  onScaleSelected?: (rows: ResourceRow[]) => void
  canWrite?: boolean
}

function renderTable(onOpen = vi.fn(), onContextMenu = vi.fn(), overrides: Overrides = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <ResourceTable
        clusterId="c1"
        typeKey="pods"
        kind="Pod"
        namespaces={['payments']}
        allNamespaces={['payments', 'storefront']}
        namespaceScoped
        onNamespacesChange={() => undefined}
        onOpen={onOpen}
        onPrefetch={() => undefined}
        onNavigate={() => undefined}
        onDismiss={() => undefined}
        onContextMenu={onContextMenu}
        selection={overrides.selection ?? new Set()}
        onSelectionChange={overrides.onSelectionChange ?? (() => undefined)}
        canWrite={overrides.canWrite ?? true}
        onCreate={() => undefined}
        onDeleteSelected={overrides.onDeleteSelected ?? (() => undefined)}
        onScaleSelected={overrides.onScaleSelected ?? (() => undefined)}
        live={false}
      />
    </QueryClientProvider>,
  )
  return onOpen
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ResourceTable', () => {
  it('renders the columns the server described', async () => {
    mockList([pod('payments-api-1'), pod('ledger-0')])
    renderTable()

    expect(await screen.findByRole('columnheader', { name: /Ready/ })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /Status/ })).toBeInTheDocument()
    expect(await screen.findByText('payments-api-1')).toBeInTheDocument()
  })

  // A status is colour first and text second: a failing row should read as failing.
  it('colours a status by what it says', async () => {
    mockList([pod('healthy'), pod('broken', { fields: { ready: '0/1', status: 'CrashLoopBackOff', restarts: '7' } })])
    renderTable()

    const failing = await screen.findByText('CrashLoopBackOff')
    expect(failing).toHaveStyle({ color: 'var(--status-error)' })

    const running = screen.getByText('Running')
    expect(running).toHaveStyle({ color: 'var(--status-ok)' })
  })

  // Filtering happens on the server: the client never holds the full set, so it could
  // not filter what it was not sent.
  it('sends the filter to the server rather than narrowing locally', async () => {
    const requests = mockList([pod('payments-api-1'), pod('ledger-0')])
    renderTable()

    await screen.findByText('payments-api-1')

    await userEvent.type(screen.getByLabelText('Filter Pod'), 'ledger')

    await waitFor(() => {
      expect(requests.some((url) => url.includes('search=ledger'))).toBe(true)
    })
    await waitFor(() => expect(screen.queryByText('payments-api-1')).not.toBeInTheDocument())
    expect(screen.getByText('ledger-0')).toBeInTheDocument()
  })

  it('asks the server to sort when a column header is used', async () => {
    const requests = mockList([pod('a'), pod('b')])
    renderTable()

    await screen.findByText('a')
    await userEvent.click(screen.getByRole('columnheader', { name: /Status/ }))

    await waitFor(() => expect(requests.some((url) => url.includes('sort=status'))).toBe(true))
  })

  it('marks unhealthy rows so a problem is visible without opening it', async () => {
    mockList([
      pod('healthy'),
      pod('pending', { severity: 'warning', fields: { ready: '0/1', status: 'Pending', restarts: '0' } }),
    ])
    renderTable()

    await screen.findByText('pending')
    expect(screen.getByLabelText('warning')).toBeInTheDocument()
  })

  it('opens the object that was clicked', async () => {
    mockList([pod('ledger-0')])
    const onOpen = renderTable()

    await userEvent.click(await screen.findByText('ledger-0'))
    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ name: 'ledger-0' }))
  })

  it('explains an empty result instead of showing a blank table', async () => {
    mockList([])
    renderTable()

    expect(await screen.findByText('No Pod')).toBeInTheDocument()
  })

  // The column belongs to the kind, not to the filter. It used to vanish as soon as a
  // single namespace was picked, so the table changed shape under the user.
  it('keeps the namespace column when one namespace is selected', async () => {
    mockList([pod('payments-api-1')])
    renderTable()

    expect(await screen.findByText('payments-api-1')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /^Namespace/ })).toBeInTheDocument()
  })

  // The reported bug: only the name was clickable, so a click on the rest of the row
  // did nothing and read as a click that had not registered.
  it('opens the object from anywhere in the row', async () => {
    mockList([pod('payments-api-1')])
    const onOpen = renderTable()

    await userEvent.click(await screen.findByText('1/1'))

    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ name: 'payments-api-1' }))
  })

  it('offers the action menu on right-click', async () => {
    mockList([pod('payments-api-1')])
    const onContextMenu = vi.fn()
    renderTable(vi.fn(), onContextMenu)

    fireEvent.contextMenu(await screen.findByText('payments-api-1'))

    const [row, at] = onContextMenu.mock.calls[0] as [{ name: string }, { x: number; y: number }]
    expect(row.name).toBe('payments-api-1')
    expect(typeof at.x).toBe('number')
  })

  it('selects a row without opening it', async () => {
    mockList([pod('payments-api-1')])
    const onSelectionChange = vi.fn()
    const onOpen = vi.fn()
    renderTable(onOpen, vi.fn(), { onSelectionChange })

    await userEvent.click(await screen.findByRole('checkbox', { name: 'Select payments-api-1' }))

    expect(onSelectionChange).toHaveBeenCalledWith(new Set(['payments/payments-api-1']))
    // Ticking a box is not the same gesture as opening the object.
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('selects every row from the header', async () => {
    mockList([pod('a'), pod('b')])
    const onSelectionChange = vi.fn()
    renderTable(vi.fn(), vi.fn(), { onSelectionChange })

    await userEvent.click(await screen.findByRole('checkbox', { name: 'Select all' }))

    expect(onSelectionChange).toHaveBeenCalledWith(new Set(['payments/a', 'payments/b']))
  })

  // Some-but-not-all is its own state: an unticked box would say nothing is selected
  // while rows below are.
  it('shows a partial selection as indeterminate', async () => {
    mockList([pod('a'), pod('b')])
    renderTable(vi.fn(), vi.fn(), { selection: new Set(['payments/a']) })

    const all = await screen.findByRole<HTMLInputElement>('checkbox', { name: 'Select all' })
    expect(all.indeterminate).toBe(true)
    expect(all.checked).toBe(false)
  })

  it('hands the delete button the rows that are selected', async () => {
    mockList([pod('a'), pod('b')])
    const onDeleteSelected = vi.fn<(rows: ResourceRow[]) => void>()
    renderTable(vi.fn(), vi.fn(), { selection: new Set(['payments/b']), onDeleteSelected })

    await userEvent.click(await screen.findByRole('button', { name: /Delete 1 selected/ }))

    expect(onDeleteSelected).toHaveBeenCalledTimes(1)
    const rows = onDeleteSelected.mock.calls[0]?.[0] ?? []
    expect(rows.map((row) => row.name)).toEqual(['b'])
  })

  it('refuses to delete when nothing is selected', async () => {
    mockList([pod('a')])
    renderTable()

    expect(await screen.findByRole('button', { name: /Select rows to delete/ })).toBeDisabled()
  })

  // Absent rather than inert: a control that can never work is clutter, not information.
  it('offers no write buttons to someone who cannot write', async () => {
    mockList([pod('a')])
    renderTable(vi.fn(), vi.fn(), { canWrite: false })

    await screen.findByText('a')
    expect(screen.queryByRole('button', { name: /Create resource/ })).not.toBeInTheDocument()
  })

  // Retyping the name of the object you just clicked adds a step without adding a
  // decision: it is on screen, and the dialog lists exactly what will go.
  it('asks before deleting without making the reader retype a name', async () => {
    mockList([pod('payments-api-1')])
    const onDeleteSelected = vi.fn<(rows: ResourceRow[]) => void>()
    renderTable(vi.fn(), vi.fn(), { selection: new Set(['payments/payments-api-1']), onDeleteSelected })

    await userEvent.click(await screen.findByRole('button', { name: /Delete 1 selected/ }))

    expect(onDeleteSelected).toHaveBeenCalledTimes(1)
  })

  // "Pending" is the question; the mark has to carry the answer, or the reader opens the
  // object to learn what the row already knew.
  it('says why a row is in trouble, not just that it is', async () => {
    mockList([
      pod('stuck', {
        fields: {
          status: 'Pending',
          restarts: '0',
          reason: 'Unschedulable',
          trouble: '0/3 nodes are available: insufficient cpu.',
        },
        severity: 'error',
      }),
    ])
    renderTable()

    const mark = await screen.findByRole('img', { name: /insufficient cpu/ })
    expect(mark).toHaveAttribute('title', expect.stringContaining('Unschedulable'))
  })

  it('names the container at fault when there is one', async () => {
    mockList([
      pod('looping', {
        fields: {
          status: 'Running',
          restarts: '6',
          reason: 'CrashLoopBackOff',
          trouble: 'The container keeps exiting.',
          troubleContainer: 'api',
        },
        severity: 'error',
      }),
    ])
    renderTable()

    expect(await screen.findByRole('img', { name: /CrashLoopBackOff · api/ })).toBeInTheDocument()
  })

  // A pod that restarted last week and is running now is working. A mark that never
  // clears is one the reader learns to scroll past, and then it is not a mark at all.
  it('leaves a running pod unmarked however many times it has restarted', async () => {
    mockList([pod('recovered', { fields: { ready: '1/1', status: 'Running', restarts: '9' } })])
    renderTable()

    expect(await screen.findByText('recovered')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: /restart/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('img', { name: /needs attention/i })).not.toBeInTheDocument()
  })

  // A kind the server has no dedicated reading for still says whether it is settled.
  it('falls back to the status when there is no reason', async () => {
    mockList([pod('unknown-state', { fields: { status: 'Terminating', restarts: '0' }, severity: 'warning' })])
    renderTable()

    expect(await screen.findByRole('img', { name: 'Pod is Terminating' })).toBeInTheDocument()
  })

  it('leaves a healthy row unmarked', async () => {
    mockList([pod('fine')])
    renderTable()

    await screen.findByText('fine')
    expect(screen.queryByRole('img', { name: /Pod is/ })).not.toBeInTheDocument()
  })

  it('opens the action menu from the row kebab', async () => {
    mockList([pod('payments-api-1')])
    const onContextMenu = vi.fn()
    renderTable(vi.fn(), onContextMenu)

    await userEvent.click(await screen.findByRole('button', { name: 'Actions for payments-api-1' }))

    expect(onContextMenu).toHaveBeenCalled()
  })
})

describe('column widths', () => {
  afterEach(() => localStorage.clear())

  it('offers a grip on every column, including the ones the kind brought with it', async () => {
    mockList([pod('one')])
    renderTable()

    expect(await screen.findByRole('separator', { name: 'Resize Name' })).toBeInTheDocument()
    for (const column of COLUMNS) {
      expect(screen.getByRole('separator', { name: `Resize ${column.label}` })).toBeInTheDocument()
    }
  })

  // The width belongs to the kind and outlives the visit; re-dragging it every time is
  // exactly the small friction that makes a tool tiring.
  it('renders a column at the width it was left at', async () => {
    localStorage.setItem('kubby.columns.Pod', JSON.stringify({ status: 320 }))
    mockList([pod('one')])
    renderTable()

    const header = await screen.findByRole('row', { name: 'Columns' })
    expect(header.style.gridTemplateColumns).toContain('320px')
  })

  it('ignores stored widths that are not widths', async () => {
    localStorage.setItem('kubby.columns.Pod', 'not json')
    mockList([pod('one')])
    renderTable()

    expect(await screen.findByText('one')).toBeInTheDocument()
  })
})

describe('container pips', () => {
  const running = (name: string, ready = true) => ({
    name,
    state: 'running' as const,
    ready,
    startedAt: '2026-08-27T18:20:55Z',
    containerId: 'containerd://d80a39fede42f882c9809cdfae6f2cdb349a4148e',
  })

  const completed = (name: string) => ({
    name,
    state: 'terminated' as const,
    ready: true,
    init: true,
    exitCode: 0,
    reason: 'Completed',
    startedAt: '2026-08-27T18:20:34Z',
    finishedAt: '2026-08-27T18:20:34Z',
  })

  // A ready count says how many containers are wrong and never which one.
  it('draws one square per container, init containers included', async () => {
    mockList([
      pod('sidecarred', {
        fields: { ready: '2/2', status: 'Running', restarts: '0', containers: '2/2' },
        containerStates: [running('fi-accounting-api'), running('istio-proxy'), completed('istio-init')],
      }),
    ])
    renderTable()

    expect(await screen.findByRole('img', { name: /fi-accounting-api — running, ready/ })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: /istio-proxy — running, ready/ })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: /istio-init — terminated, ready: Completed/ })).toBeInTheDocument()
  })

  it('summarises the container under the pointer', async () => {
    mockList([
      pod('sidecarred', {
        fields: { ready: '1/1', status: 'Running', restarts: '0', containers: '1/1' },
        containerStates: [completed('istio-init')],
      }),
    ])
    renderTable()

    fireEvent.mouseEnter(await screen.findByRole('img', { name: /istio-init/ }))

    const card = await screen.findByRole('tooltip')
    expect(card).toHaveTextContent('istio-init')
    expect(card).toHaveTextContent('terminated, ready')
    // Zero is a meaningful exit code and has to be shown, not treated as absent.
    expect(card).toHaveTextContent('Exit Code')
    expect(card).toHaveTextContent('0')
    expect(card).toHaveTextContent('Completed')

    fireEvent.mouseLeave(screen.getByRole('img', { name: /istio-init/ }))
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  // A row from a cache written before the states existed still has to draw something.
  it('falls back to the ready count when a row carries no states', async () => {
    mockList([pod('old', { fields: { ready: '1/2', status: 'Running', restarts: '0', containers: '1/2' } })])
    renderTable()

    expect(await screen.findByTitle('1 of 2 containers ready')).toBeInTheDocument()
  })
})

describe('log findings', () => {
  const finding = (over: Record<string, unknown> = {}) => ({
    namespace: 'nx-apps',
    pod: 'nx-fxrateengineapi-ff6b97dfd-8mlwz',
    rule: 'SQL Server',
    class: 'auth' as const,
    count: 456672,
    summary: 'database NetTrexCommon · user netixuser',
    sample: 'Cannot open database "NetTrexCommon" requested by the login. The login failed.',
    firstSeen: '2026-08-29T22:00:00Z',
    lastSeen: '2026-08-29T22:14:00Z',
    severity: 'error' as const,
    ...over,
  })

  it('marks a row whose logs keep reporting a problem', async () => {
    mockList([pod('running-but-broken', { logFinding: finding() })])
    renderTable()

    expect(await screen.findByRole('img', { name: /Logs report SQL Server/ })).toBeInTheDocument()
  })

  // The pod is Running and Ready and Kubernetes has nothing to say about it. That is
  // the whole case for reading the logs at all.
  it('marks it even though the object itself looks healthy', async () => {
    mockList([pod('running-but-broken', { logFinding: finding() })])
    renderTable()

    await screen.findByRole('img', { name: /Logs report/ })
    // No Kubernetes warning triangle: nothing about the object is wrong.
    expect(screen.queryByRole('img', { name: /needs attention|Pod is/ })).not.toBeInTheDocument()
  })

  it('leaves a row with nothing in its logs unmarked', async () => {
    mockList([pod('quiet')])
    renderTable()

    expect(await screen.findByText('quiet')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: /Logs report/ })).not.toBeInTheDocument()
  })

  it('says what the logs said and for how long', async () => {
    mockList([pod('running-but-broken', { logFinding: finding() })])
    renderTable()

    fireEvent.mouseEnter(await screen.findByRole('img', { name: /Logs report/ }))

    const card = await screen.findByRole('tooltip')
    expect(card).toHaveTextContent('SQL Server')
    expect(card).toHaveTextContent('456,672 lines')
    expect(card).toHaveTextContent('database NetTrexCommon')
    expect(card).toHaveTextContent('Cannot open database')
    expect(card).toHaveTextContent(/first seen/)
  })

  // Nine replicas saying the same thing is the same news nine times.
  it('says how many pods a rolled-up finding covers', async () => {
    mockList([
      pod('fi-n8n-worker', {
        logFinding: finding({ rule: 'Connection refused', class: 'unreachable', pods: 9, count: 63 }),
      }),
    ])
    renderTable()

    fireEvent.mouseEnter(await screen.findByRole('img', { name: /across 9 pods/ }))
    expect(await screen.findByRole('tooltip')).toHaveTextContent('9 pods')
  })
})
