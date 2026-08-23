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

const CLUSTER = {
  id: 'c1', name: 'test', environment: 'test', environmentLabel: '',
  displayEnvironment: 'test', color: '', authSource: 'kubeconfig',
  apiServerUrl: 'https://127.0.0.1:6550', insecureSkipTlsVerify: false,
  credentialStatus: 'valid', k8sVersion: 'v1.35.5+k3s1', nodeCount: 3,
  metricsAvailable: true, readOnly: false, impersonationEnabled: false, qpsLimit: 50,
}

const TYPES = [
  { key: 'pods', kind: 'Pod', namespaced: true, category: 'workload', cached: true },
  { key: 'apps/deployments', kind: 'Deployment', namespaced: true, category: 'workload', cached: true },
]

function mockApi() {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      const json = (body: unknown) =>
        Promise.resolve(new Response(JSON.stringify(body), {
          status: 200, headers: { 'Content-Type': 'application/json' },
        }))

      if (url.includes('/resource-types')) return json({ types: TYPES })
      if (url.includes('/namespaces')) return json({ namespaces: ['default', 'payments', 'storefront'] })
      if (url.includes('/object/')) {
        return json({ apiVersion: 'apps/v1', kind: 'Deployment', metadata: { name: 'payments-api', namespace: 'payments' } })
      }
      if (url.includes('/resources/')) {
        return json({
          columns: [{ key: 'ready', label: 'Ready' }],
          rows: [{
            name: 'payments-api', namespace: 'payments', age: '2d',
            createdAt: '2026-08-21T10:00:00Z', fields: { ready: '3/3' },
          }],
          total: 1, fromCache: true,
        })
      }
      if (url.includes('/api/v1/clusters')) return json({ clusters: [CLUSTER] })
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

describe('browsing history', () => {
  // The reported bug: opening an object then pressing back left the cluster entirely,
  // because none of the browsing state was in the URL.
  it('steps back from an object to its list, not out of the cluster', async () => {
    window.history.replaceState(null, '', '/clusters/c1/payments/apps/deployments')
    renderShell()

    await userEvent.click(await screen.findByText('payments-api'))
    await waitFor(() =>
      expect(window.location.pathname + window.location.search).toBe(
        '/clusters/c1/payments/apps/deployments?object=payments-api&ns=payments',
      ),
    )

    window.history.back()
    await waitFor(() =>
      expect(window.location.pathname).toBe('/clusters/c1/payments/apps/deployments'),
    )
    expect(await screen.findByText('payments-api')).toBeInTheDocument()
  })

  it('gives the kind its own URL', async () => {
    window.history.replaceState(null, '', '/clusters/c1/payments/pods')
    renderShell()

    await userEvent.click(await screen.findByRole('button', { name: /Deployment/ }))
    await waitFor(() =>
      expect(window.location.pathname).toBe('/clusters/c1/payments/apps/deployments'),
    )

    window.history.back()
    await waitFor(() => expect(window.location.pathname).toBe('/clusters/c1/payments/pods'))
  })

  // Namespace selection lives above the list it narrows, and several are allowed.
  it('records the chosen namespaces in the URL', async () => {
    window.history.replaceState(null, '', '/clusters/c1/payments/pods')
    renderShell()

    await userEvent.click(await screen.findByRole('button', { name: 'Namespace' }))
    await userEvent.click(screen.getByRole('option', { name: 'storefront' }))

    await waitFor(() =>
      expect(decodeURIComponent(window.location.pathname)).toBe(
        '/clusters/c1/payments,storefront/pods',
      ),
    )
  })

  it('restores the exact view named by the URL on load', async () => {
    window.history.replaceState(null, '', '/clusters/c1/payments/apps/deployments?object=payments-api')
    renderShell()

    // The object opens directly, without going through the list first.
    expect(await screen.findByRole('tab', { name: 'YAML' })).toBeInTheDocument()
  })
})
