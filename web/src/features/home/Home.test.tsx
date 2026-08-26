import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { Home } from './Home'

const CONNECTED = {
  id: 'c1',
  name: 'prod-app',
  environment: 'Production',
  colour: '',
  status: 'valid',
  unreachable: false,
  counts: { critical: 2, warning: 1 },
  top: [
    {
      category: 'workload',
      kind: 'Pod',
      namespace: 'shop',
      name: 'web-1',
      reason: 'ImagePullBackOff',
      detail: 'the image could not be pulled',
      severity: 'critical',
    },
  ],
  checkedAt: '2026-08-26T10:00:00Z',
  stale: false,
  capacity: { nodes: 3, nodesReady: 3, cores: 12, memoryMiB: 47088, pods: 330, k8sVersion: 'v1.34.4' },
}

const UNREACHABLE = {
  id: 'c2',
  name: 'prod-rancher',
  environment: 'Production',
  colour: '',
  status: 'unreachable',
  unreachable: true,
  error: 'dial tcp: lookup prod-rancher.internal: no such host',
  counts: {},
  checkedAt: '2026-08-26T10:00:00Z',
  stale: false,
}

function renderHome(clusters: unknown[] = [CONNECTED, UNREACHABLE]) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ clusters }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    ),
  )

  const onOpen = vi.fn()
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <Home onOpen={onOpen} />
    </QueryClientProvider>,
  )
  return { onOpen }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Home', () => {
  // The one fact the card exists to carry, in words as well as colour.
  it('says whether each cluster is connected', async () => {
    renderHome()

    await waitFor(() => expect(screen.getByText('Connected')).toBeInTheDocument())
    expect(screen.getByText('Not connected')).toBeInTheDocument()
    expect(screen.getByText('1 of 2 connected')).toBeInTheDocument()
  })

  // What is on the other end of the link, before anyone clicks it.
  it('says how big a connected cluster is', async () => {
    renderHome()

    await waitFor(() => expect(screen.getByText('3')).toBeInTheDocument())
    expect(screen.getByText('nodes')).toBeInTheDocument()
    expect(screen.getByText('12')).toBeInTheDocument()
    expect(screen.getByText('46.0 GiB')).toBeInTheDocument()
    expect(screen.getByText('v1.34.4')).toBeInTheDocument()
  })

  it('opens the cluster a card names', async () => {
    const { onOpen } = renderHome()

    await userEvent.click(await screen.findByRole('button', { name: /prod-app/ }))
    expect(onOpen).toHaveBeenCalledWith('c1')
  })

  // The trap this screen exists to prevent: opening a cluster nobody can talk to lands
  // the reader on an error page, and every screen under it shows the same error.
  it('does not offer a click on a cluster it cannot reach', async () => {
    renderHome()

    await waitFor(() => expect(screen.getByText('Not connected')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /prod-rancher/ })).not.toBeInTheDocument()
    // And says why, rather than only that it failed.
    expect(screen.getByText(/no such host/)).toBeInTheDocument()
  })

  // A card with no capacity block is a card that could not be measured, not one with
  // nothing in it.
  it('writes a dash where the size could not be read', async () => {
    renderHome([{ ...CONNECTED, capacity: null }])

    await waitFor(() => expect(screen.getByText('Connected')).toBeInTheDocument())
    expect(screen.getAllByText('—').length).toBe(4)
  })
})
