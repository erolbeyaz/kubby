import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'

const SIGNED_IN_ME = {
  user: {
    id: 'u-1',
    email: 'admin@example.com',
    displayName: 'First Admin',
    role: 'admin',
    isActive: true,
    mfaEnrolled: true,
    createdAt: '2026-08-22T10:00:00Z',
  },
  permissions: ['cluster.read', 'user.manage', 'audit.read'],
  mfaEnrolled: true,
  readOnly: false,
}

/** Mock server that flips /me to 401 once logout has been called, like the real API. */
function mockSignedInApi() {
  let signedIn = true
  const logoutCalls: string[] = []

  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = input instanceof Request ? input.url : input.toString()
      const json = (body: unknown, status = 200) =>
        Promise.resolve(
          new Response(status === 204 ? null : JSON.stringify(body), {
            status,
            headers: { 'Content-Type': 'application/json' },
          }),
        )

      if (url.includes('/auth/logout')) {
        logoutCalls.push(init?.method ?? 'GET')
        signedIn = false
        return json(null, 204)
      }
      if (url.includes('/api/v1/setup/status')) return json({ setupRequired: false })
      if (url.includes('/api/v1/me/sessions')) return json({ sessions: [] })
      if (url.includes('/api/v1/me')) {
        return signedIn ? json(SIGNED_IN_ME) : json({ error: 'authentication required' }, 401)
      }
      if (url.includes('/readyz')) return json({ status: 'ok' })
      if (url.includes('/version')) {
        return json({ version: 'dev', commitSha: 'abc1234', buildDate: 'x', goVersion: 'go1.27.0' })
      }
      return json({ error: 'not found' }, 404)
    }),
  )

  return { logoutCalls }
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

describe('signing out', () => {
  // The click must not wait on the network: the workspace goes away first and the
  // request follows.
  it('leaves the workspace before the request resolves', async () => {
    let releaseLogout: () => void = () => undefined
    const logoutPending = new Promise<void>((resolve) => {
      releaseLogout = resolve
    })

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = input instanceof Request ? input.url : input.toString()
        const json = (body: unknown, status = 200) =>
          new Response(status === 204 ? null : JSON.stringify(body), {
            status,
            headers: { 'Content-Type': 'application/json' },
          })

        if (url.includes('/auth/logout')) {
          await logoutPending
          return json(null, 204)
        }
        if (url.includes('/api/v1/setup/status')) return json({ setupRequired: false })
        if (url.includes('/api/v1/me')) return json(SIGNED_IN_ME)
        if (url.includes('/readyz')) return json({ status: 'ok' })
        if (url.includes('/version')) {
          return json({ version: 'dev', commitSha: 'abc1234', buildDate: 'x', goVersion: 'go1.27.0' })
        }
        return json({ error: 'not found' }, 404)
      }),
    )

    renderApp()

    await userEvent.click(await screen.findByRole('button', { name: /account: first admin/i }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Sign out' }))

    // Still in flight, yet the sign-in screen is already showing.
    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeInTheDocument()

    releaseLogout()
  })

  it('warns when the sign-out request could not reach the server', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = input instanceof Request ? input.url : input.toString()
        const json = (body: unknown, status = 200) =>
          Promise.resolve(
            new Response(JSON.stringify(body), {
              status,
              headers: { 'Content-Type': 'application/json' },
            }),
          )

        if (url.includes('/auth/logout')) return Promise.reject(new Error('network down'))
        if (url.includes('/api/v1/setup/status')) return json({ setupRequired: false })
        if (url.includes('/api/v1/me')) return json(SIGNED_IN_ME)
        if (url.includes('/readyz')) return json({ status: 'ok' })
        if (url.includes('/version')) {
          return json({ version: 'dev', commitSha: 'abc1234', buildDate: 'x', goVersion: 'go1.27.0' })
        }
        return json({ error: 'not found' }, 404)
      }),
    )

    renderApp()

    await userEvent.click(await screen.findByRole('button', { name: /account: first admin/i }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Sign out' }))

    // The user must not believe they signed out on a machine where they did not.
    expect(await screen.findByText(/sign-out may not have completed/i)).toBeInTheDocument()
  })

  it('calls the API and returns the user to the sign-in screen', async () => {
    const { logoutCalls } = mockSignedInApi()
    renderApp()

    await userEvent.click(await screen.findByRole('button', { name: /account: first admin/i }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Sign out' }))

    await waitFor(() => expect(logoutCalls).toHaveLength(1))
    expect(logoutCalls[0]).toBe('POST')

    // The whole point: the workspace must actually disappear.
    await waitFor(
      () => expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument(),
      { timeout: 3000 },
    )
    expect(screen.queryByRole('button', { name: /account: first admin/i })).not.toBeInTheDocument()
  })
})
