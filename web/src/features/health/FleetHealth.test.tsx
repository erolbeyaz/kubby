import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { FleetHealth } from './FleetHealth'

const HEALTHY = {
  id: 'c1',
  name: 'kubby-mini',
  environment: 'test',
  colour: '',
  status: 'valid',
  unreachable: false,
  counts: { critical: 1, warning: 0 },
  checkedAt: '2026-08-26T10:00:00Z',
  stale: false,
}

const UNREACHABLE = {
  id: 'c2',
  name: 'prod-app',
  environment: 'Production',
  colour: '',
  status: 'unreachable',
  unreachable: true,
  error: 'dial tcp: lookup prod-rancher.internal: no such host',
  counts: {},
  checkedAt: '2026-08-26T10:00:00Z',
  stale: false,
}

function renderStrip(clusters: unknown[] = [HEALTHY, UNREACHABLE]) {
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
      <FleetHealth onOpen={onOpen} currentId="c1" />
    </QueryClientProvider>,
  )
  return { onOpen }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('FleetHealth strip', () => {
  // The same rule the Home cards follow: there is nothing behind a cluster nobody can
  // reach, and offering the click lands the reader on an error page with no way back.
  it('does not offer a click on a cluster it cannot reach', async () => {
    renderStrip()

    await waitFor(() => expect(screen.getByText('Not connected')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /prod-app/ })).not.toBeInTheDocument()
    // The reachable one is still a button.
    expect(screen.getByRole('button', { name: /kubby-mini/ })).toBeInTheDocument()
  })

  // A grey edge on its own does not distinguish "quiet" from "nobody can reach it".
  it('says why in words, not only in colour', async () => {
    renderStrip()

    await waitFor(() => expect(screen.getByText('Not connected')).toBeInTheDocument())
    expect(screen.getByTitle(/no such host/)).toBeInTheDocument()
  })

  // One cluster on its own is not a fleet.
  it('shows nothing when there is only one cluster', () => {
    const { container } = render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <FleetHealth onOpen={vi.fn()} />
      </QueryClientProvider>,
    )
    expect(container.querySelector('section')).toBeNull()
  })
})
