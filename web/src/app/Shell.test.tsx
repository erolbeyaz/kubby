import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Me } from '@/lib/api'

import { Shell } from './Shell'

const ADMIN: Me = {
  user: {
    id: 'u-1',
    email: 'admin@example.com',
    displayName: 'First Admin',
    role: 'admin',
    isActive: true,
    mfaEnrolled: false,
    createdAt: '2026-08-22T10:00:00Z',
  },
  permissions: ['cluster.read', 'cluster.write', 'cluster.manage', 'user.manage', 'audit.read'],
  mfaEnrolled: false,
  readOnly: false,
}

const VIEWER: Me = {
  ...ADMIN,
  user: { ...ADMIN.user, id: 'u-2', email: 'viewer@example.com', role: 'readonly' },
  permissions: ['cluster.read'],
}

function renderShell(me: Me = ADMIN) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <Shell me={me} onSignOut={() => undefined} />
    </QueryClientProvider>,
  )
}

function mockApi(readyz: { status: number; body: unknown }) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      if (url.includes('/version')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              version: '0.1.0',
              commitSha: 'abc1234',
              buildDate: '2026-08-22T00:00:00Z',
              goVersion: 'go1.27.0',
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        )
      }
      return Promise.resolve(
        new Response(JSON.stringify(readyz.body), {
          status: readyz.status,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Shell', () => {
  it('renders the primary navigation and the workspace chrome', () => {
    mockApi({ status: 200, body: { status: 'ok' } })
    renderShell()

    expect(screen.getByRole('navigation', { name: 'Primary' })).toBeInTheDocument()
    expect(screen.getByRole('tablist', { name: 'Workspace tabs' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clusters' })).toBeInTheDocument()
  })

  it('puts the identity and account actions behind the top-right menu', async () => {
    mockApi({ status: 200, body: { status: 'ok' } })
    renderShell()

    // Collapsed: the avatar shows initials and nothing else leaks into the chrome.
    const trigger = screen.getByRole('button', { name: /account: first admin/i })
    expect(trigger).toHaveTextContent('FA')
    expect(screen.queryByText('admin@example.com')).not.toBeInTheDocument()

    await userEvent.click(trigger)

    expect(screen.getByText('First Admin')).toBeInTheDocument()
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    for (const label of ['Change password', 'Two-factor authentication', 'Active sessions']) {
      expect(screen.getByRole('menuitem', { name: label })).toBeInTheDocument()
    }
    expect(screen.getByRole('menuitem', { name: 'Sign out' })).toBeInTheDocument()
  })

  it('closes the account menu on Escape', async () => {
    mockApi({ status: 200, body: { status: 'ok' } })
    renderShell()

    await userEvent.click(screen.getByRole('button', { name: /account: first admin/i }))
    expect(screen.getByRole('menu')).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('reports Ready and shows the build identity once the API answers', async () => {
    mockApi({ status: 200, body: { status: 'ok', checks: { database: 'ok' } } })
    renderShell()

    await waitFor(() => expect(screen.getByText('Ready')).toBeInTheDocument())
    expect(await screen.findByText(/abc1234/)).toBeInTheDocument()
  })

  // The panel entry is hidden for a readonly user, but the server is what actually
  // refuses the request — this only checks that the UI does not advertise it.
  it('hides the user-management entry from a readonly user', async () => {
    mockApi({ status: 200, body: { status: 'ok' } })
    const { rerender } = renderShell(VIEWER)

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    expect(screen.queryByRole('button', { name: 'Users' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Account' })).toBeInTheDocument()

    rerender(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <Shell me={ADMIN} onSignOut={() => undefined} />
      </QueryClientProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    expect(screen.getByRole('button', { name: 'Users' })).toBeInTheDocument()
  })

  it('reports Offline instead of a blank screen when the API is unreachable', async () => {
    mockApi({ status: 503, body: { status: 'unavailable', detail: 'database is not reachable' } })
    renderShell()

    await waitFor(() => expect(screen.getByText('Offline')).toBeInTheDocument())
  })
})
