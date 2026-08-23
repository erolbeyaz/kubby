import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'

type Route = { status: number; body: unknown }

function mockRoutes(routes: Record<string, Route>) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      const match = Object.keys(routes).find((key) => url.includes(key))
      const route = match ? routes[match] : undefined

      if (!route) {
        return Promise.resolve(new Response('{}', { status: 404 }))
      }
      return Promise.resolve(
        new Response(JSON.stringify(route.body), {
          status: route.status,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }),
  )
}

function renderApp() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App routing by session state', () => {
  it('shows the setup wizard on a fresh installation', async () => {
    mockRoutes({ '/api/v1/setup/status': { status: 200, body: { setupRequired: true } } })
    renderApp()

    expect(await screen.findByText('Create the first administrator')).toBeInTheDocument()
  })

  // A signed-out visitor is a normal state, not an error.
  it('shows the sign-in screen when setup is done but nobody is signed in', async () => {
    mockRoutes({
      '/api/v1/setup/status': { status: 200, body: { setupRequired: false } },
      '/api/v1/me': { status: 401, body: { error: 'authentication required' } },
    })
    renderApp()

    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeInTheDocument()
  })

  it('shows the workspace once a session resolves', async () => {
    mockRoutes({
      '/api/v1/setup/status': { status: 200, body: { setupRequired: false } },
      '/api/v1/me': {
        status: 200,
        body: {
          user: {
            id: 'u-1',
            email: 'admin@example.com',
            displayName: 'First Admin',
            role: 'admin',
            isActive: true,
            mfaEnrolled: false,
            createdAt: '2026-08-22T10:00:00Z',
          },
          permissions: ['cluster.read', 'user.manage'],
          mfaEnrolled: false,
          readOnly: false,
        },
      },
      '/api/v1/clusters': { status: 200, body: { clusters: [] } },
      '/readyz': { status: 200, body: { status: 'ok' } },
      '/version': {
        status: 200,
        body: { version: 'dev', commitSha: 'abc1234', buildDate: 'x', goVersion: 'go1.27.0' },
      },
    })
    renderApp()

    // The workspace is up once its header is: the cluster picker lives on the rail
    // inside a cluster, and this session has none.
    await waitFor(() => expect(screen.getByRole('button', { name: 'Overview' })).toBeInTheDocument())
    expect(screen.getByRole('button', { name: /account: first admin/i })).toBeInTheDocument()
  })

  it('reports an unreachable API instead of rendering an empty page', async () => {
    mockRoutes({ '/api/v1/setup/status': { status: 500, body: { error: 'boom' } } })
    renderApp()

    expect(await screen.findByText('Kubby is unreachable')).toBeInTheDocument()
  })
})
