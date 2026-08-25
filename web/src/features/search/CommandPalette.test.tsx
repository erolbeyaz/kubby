import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type SearchHit, type SearchResult } from '@/lib/api'

import { CommandPalette } from './CommandPalette'

function hit(over: Partial<SearchHit> = {}): SearchHit {
  return {
    clusterId: 'c1',
    clusterName: 'prod-eu',
    environment: 'prod',
    typeKey: 'pods',
    kind: 'Pod',
    namespace: 'payments',
    name: 'payments-api-abc',
    status: 'Running',
    ...over,
  }
}

function show(result: SearchResult, onOpen = vi.fn()) {
  vi.spyOn(api, 'search').mockResolvedValue(result)

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <CommandPalette onClose={vi.fn()} onOpen={onOpen} />
    </QueryClientProvider>,
  )
  return { onOpen }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('CommandPalette', () => {
  it('shows which cluster each result is in', async () => {
    show({
      hits: [hit(), hit({ clusterId: 'c2', clusterName: 'test-eu', name: 'payments-worker' })],
      truncated: false,
    })

    await userEvent.type(screen.getByLabelText('Search every cluster'), 'payments')

    await waitFor(() => expect(screen.getByText('payments-api-abc')).toBeInTheDocument())
    // In a fleet the first thing worth knowing about a result is where it lives.
    expect(screen.getByText('prod-eu')).toBeInTheDocument()
    expect(screen.getByText('test-eu')).toBeInTheDocument()
  })

  // The important one. A search that quietly returns fewer results because a cluster is
  // down is a search that lies: the reader concludes the object does not exist.
  it('says when a cluster could not be searched', async () => {
    show({
      hits: [hit()],
      unreachable: [{ clusterId: 'c9', clusterName: 'prod-us', reason: 'connection refused' }],
      truncated: false,
    })

    await userEvent.type(screen.getByLabelText('Search every cluster'), 'payments')

    await waitFor(() => expect(screen.getByText(/Not searched/)).toBeInTheDocument())
    expect(screen.getByText(/prod-us/)).toBeInTheDocument()
    expect(screen.getByText(/results may be incomplete/)).toBeInTheDocument()
  })

  it('opens the highlighted result on Enter', async () => {
    const { onOpen } = show({
      hits: [hit({ name: 'first' }), hit({ name: 'second' }), hit({ name: 'third' })],
      truncated: false,
    })

    const box = screen.getByLabelText('Search every cluster')
    await userEvent.type(box, 'pay')
    await waitFor(() => expect(screen.getByText('first')).toBeInTheDocument())

    await userEvent.keyboard('{ArrowDown}{ArrowDown}{Enter}')

    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onOpen.mock.calls[0]?.[0]).toMatchObject({ name: 'third' })
  })

  it('does not run past the ends of the list', async () => {
    const { onOpen } = show({ hits: [hit({ name: 'only' })], truncated: false })

    await userEvent.type(screen.getByLabelText('Search every cluster'), 'only')
    await waitFor(() => expect(screen.getByText('only')).toBeInTheDocument())

    // Far more presses than there are rows, in both directions.
    await userEvent.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}{ArrowUp}{ArrowUp}{Enter}')

    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onOpen.mock.calls[0]?.[0]).toMatchObject({ name: 'only' })
  })

  // Below two characters nearly everything matches, and every keystroke fans out to a
  // list call on every API server in the fleet.
  it('asks for nothing until the query is worth running', async () => {
    const search = vi.spyOn(api, 'search').mockResolvedValue({ hits: [], truncated: false })

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <CommandPalette onClose={vi.fn()} onOpen={vi.fn()} />
      </QueryClientProvider>,
    )

    await userEvent.type(screen.getByLabelText('Search every cluster'), 'p')

    await waitFor(() => expect(screen.getByText(/at least two characters/)).toBeInTheDocument())
    expect(search).not.toHaveBeenCalled()
  })

  it('says so plainly when nothing matched', async () => {
    show({ hits: [], truncated: false })

    await userEvent.type(screen.getByLabelText('Search every cluster'), 'nothinghere')

    await waitFor(() => expect(screen.getByText(/Nothing matched/)).toBeInTheDocument())
  })
})
