import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ResourceTable } from './ResourceTable'

const COLUMNS = [
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

function renderTable(onOpen = vi.fn()) {
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
})
