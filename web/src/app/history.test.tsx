import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Me } from '@/lib/api'

import { Shell } from './Shell'

const ADMIN: Me = {
  user: {
    id: 'u1', email: 'admin@example.com', displayName: 'First Admin', role: 'admin',
    isActive: true, mfaEnrolled: true, createdAt: '2026-08-22T10:00:00Z',
  },
  permissions: ['cluster.read', 'cluster.write', 'cluster.manage', 'user.manage', 'audit.read'],
  mfaEnrolled: true,
  readOnly: false,
}

function mockApi() {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      const json = (body: unknown) =>
        Promise.resolve(new Response(JSON.stringify(body), {
          status: 200, headers: { 'Content-Type': 'application/json' },
        }))

      if (url.includes('/clusters')) return json({ clusters: [] })
      if (url.includes('/users')) return json({ users: [] })
      if (url.includes('/me/sessions')) return json({ sessions: [] })
      if (url.includes('/readyz')) return json({ status: 'ok' })
      if (url.includes('/version')) {
        return json({ version: 'dev', commitSha: 'abc', buildDate: 'x', goVersion: 'go1.27.0' })
      }
      return json({})
    }),
  )
}

function renderShell() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <Shell me={ADMIN} onSignOut={() => undefined} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  window.history.replaceState(null, '', '/clusters')
  mockApi()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('workspace navigation', () => {
  it('gives every section its own URL', async () => {
    renderShell()

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    expect(window.location.pathname).toBe('/settings/account')

    await userEvent.click(screen.getByRole('button', { name: 'Users' }))
    expect(window.location.pathname).toBe('/settings/users')

    await userEvent.click(screen.getByRole('button', { name: 'Clusters' }))
    expect(window.location.pathname).toBe('/clusters')
  })

  // The complaint that started this: back used to leave the application entirely.
  it('steps back through the application rather than leaving it', async () => {
    renderShell()

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    await userEvent.click(screen.getByRole('button', { name: 'Users' }))
    expect(window.location.pathname).toBe('/settings/users')

    window.history.back()
    await waitFor(() => expect(window.location.pathname).toBe('/settings/account'))

    window.history.back()
    await waitFor(() => expect(window.location.pathname).toBe('/clusters'))
  })

  it('restores the view named by the URL on load', () => {
    window.history.replaceState(null, '', '/settings/users')
    renderShell()

    expect(screen.getByRole('tab', { name: /users/i })).toBeInTheDocument()
  })

  it('does not stack history entries for the section already open', async () => {
    renderShell()

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    const depth = window.history.length

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    expect(window.history.length).toBe(depth)
  })
})
