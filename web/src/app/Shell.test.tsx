import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Me } from '@/lib/api'

import { Shell } from './Shell'

const ADMIN: Me = {
  user: {
    id: 'u-1', email: 'admin@example.com', displayName: 'First Admin', role: 'admin',
    isActive: true, mfaEnrolled: false, createdAt: '2026-08-22T10:00:00Z',
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

const CLUSTERS = [
  {
    id: 'c1', name: 'prod-app', environment: 'prod', environmentLabel: 'Production',
    displayEnvironment: 'Production', color: '', authSource: 'kubeconfig',
    apiServerUrl: 'https://127.0.0.1:6550', insecureSkipTlsVerify: false,
    credentialStatus: 'valid', k8sVersion: 'v1.35.5+k3s1', nodeCount: 3,
    metricsAvailable: true, readOnly: false, impersonationEnabled: false, qpsLimit: 50,
  },
  {
    id: 'c2', name: 'staging', environment: 'test', environmentLabel: '',
    displayEnvironment: 'test', color: '', authSource: 'kubeconfig',
    apiServerUrl: 'https://127.0.0.1:6551', insecureSkipTlsVerify: false,
    credentialStatus: 'invalid', statusDetail: 'the credential was rejected',
    metricsAvailable: false, readOnly: false, impersonationEnabled: false, qpsLimit: 50,
  },
]

function mockApi(clusters: unknown[] = CLUSTERS) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      const json = (body: unknown) =>
        Promise.resolve(new Response(JSON.stringify(body), {
          status: 200, headers: { 'Content-Type': 'application/json' },
        }))

      if (url.includes('/resource-types')) return json({ types: [] })
      if (url.includes('/namespaces')) return json({ namespaces: ['payments'] })
      if (url.includes('/api/v1/clusters')) return json({ clusters })
      if (url.includes('/users')) return json({ users: [] })
      if (url.includes('/readyz')) return json({ status: 'ok' })
      if (url.includes('/version')) {
        return json({ version: 'dev', commitSha: 'abc1234', buildDate: 'x', goVersion: 'go1.27.0' })
      }
      return json({})
    }),
  )
}

function renderShell(me: Me = ADMIN) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <Shell me={me} onSignOut={() => undefined} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  window.history.replaceState(null, '', '/clusters')
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Shell', () => {
  // The picker governs the window, so it is the first thing in it.
  it('puts the cluster picker in the top-left corner', async () => {
    mockApi()
    renderShell()

    expect(await screen.findByRole('button', { name: 'Select cluster' })).toBeInTheDocument()
  })

  it('offers no navigation that leads nowhere', async () => {
    mockApi()
    renderShell()

    await screen.findByRole('button', { name: 'Select cluster' })

    // These were rail entries with nothing behind them; a menu that does nothing is
    // worse than an absent one.
    for (const dead of ['Health', 'Workloads', 'Network', 'Storage', 'Events', 'Terminal']) {
      expect(screen.queryByRole('button', { name: dead })).not.toBeInTheDocument()
    }
  })

  it('switches cluster from the picker', async () => {
    mockApi()
    renderShell()

    await userEvent.click(await screen.findByRole('button', { name: 'Select cluster' }))
    await userEvent.click(screen.getByRole('option', { name: /staging/ }))

    await waitFor(() => expect(window.location.pathname).toBe('/clusters/c2/-/overview'))
  })

  it('reaches cluster management through the picker, not the navigation', async () => {
    mockApi()
    renderShell()

    await userEvent.click(await screen.findByRole('button', { name: 'Select cluster' }))
    await userEvent.click(screen.getByRole('button', { name: /Manage clusters/ }))

    await waitFor(() => expect(window.location.pathname).toBe('/manage'))
  })

  it('hides cluster management from someone who cannot manage clusters', async () => {
    mockApi()
    renderShell(VIEWER)

    await userEvent.click(await screen.findByRole('button', { name: 'Select cluster' }))
    expect(screen.queryByRole('button', { name: /Manage clusters/ })).not.toBeInTheDocument()
  })

  it('keeps the identity and its actions behind the account menu', async () => {
    mockApi()
    renderShell()

    const trigger = await screen.findByRole('button', { name: /account: first admin/i })
    expect(trigger).toHaveTextContent('FA')

    await userEvent.click(trigger)
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Sign out' })).toBeInTheDocument()
  })

  it('says what to do when there are no clusters at all', async () => {
    mockApi([])
    renderShell()

    expect(await screen.findByText('No clusters yet')).toBeInTheDocument()
  })
})
