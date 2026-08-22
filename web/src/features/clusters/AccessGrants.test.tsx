import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/lib/api'

import { ClusterDetail } from './ClusterDetail'

const CLUSTER: Cluster = {
  id: 'c1',
  name: 'test',
  environment: 'test',
  environmentLabel: '',
  displayEnvironment: 'test',
  color: '',
  authSource: 'kubeconfig',
  apiServerUrl: 'https://127.0.0.1:6550',
  insecureSkipTlsVerify: false,
  credentialStatus: 'valid',
  k8sVersion: 'v1.35.5+k3s1',
  nodeCount: 3,
  metricsAvailable: true,
  readOnly: false,
  impersonationEnabled: false,
  qpsLimit: 50,
}

const MEMBER = {
  id: 'u2', email: 'member@example.com', displayName: 'Member', role: 'user',
  isActive: true, mfaEnrolled: false, createdAt: '2026-08-22T10:00:00Z',
}

/** Grants respond slowly so the test can observe what the control shows meanwhile. */
function mockApi(grantDelayMs: number) {
  let grants: unknown[] = []

  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = input instanceof Request ? input.url : input.toString()
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })

      if (url.includes('/grants')) {
        if (init?.method === 'PUT') {
          const raw = typeof init.body === 'string' ? init.body : '{}'
          const body = JSON.parse(raw) as { userId: string; accessLevel: string }
          await new Promise((resolve) => setTimeout(resolve, grantDelayMs))
          grants = body.accessLevel
            ? [{ userId: body.userId, email: MEMBER.email, displayName: MEMBER.displayName, accessLevel: body.accessLevel }]
            : []
        }
        return json({ grants })
      }
      if (url.includes('/users')) return json({ users: [MEMBER] })
      return json({})
    }),
  )
}

function renderDetail() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <ClusterDetail cluster={CLUSTER} canManage onBack={() => undefined} />
    </QueryClientProvider>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('cluster access grants', () => {
  // The reported symptom: the dropdown showed the old value for a beat, then jumped.
  it('shows the chosen level immediately, without waiting for the server', async () => {
    mockApi(500)
    renderDetail()

    const select = await screen.findByLabelText('Access for member@example.com')
    expect(select).toHaveValue('')

    await userEvent.selectOptions(select, 'read')

    // Still in flight, yet the control already reads "read".
    expect(select).toHaveValue('read')
    await waitFor(() => expect(select).toHaveValue('read'), { timeout: 2000 })
  })

  it('puts the old level back when the server refuses', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = input instanceof Request ? input.url : input.toString()
        const json = (body: unknown, status = 200) =>
          Promise.resolve(new Response(JSON.stringify(body), {
            status, headers: { 'Content-Type': 'application/json' },
          }))

        if (url.includes('/grants') && init?.method === 'PUT') {
          return json({ error: 'nope' }, 403)
        }
        if (url.includes('/grants')) return json({ grants: [] })
        if (url.includes('/users')) return json({ users: [MEMBER] })
        return json({})
      }),
    )
    renderDetail()

    const select = await screen.findByLabelText('Access for member@example.com')
    await userEvent.selectOptions(select, 'write')

    // A change that did not happen must not be left on screen.
    await waitFor(() => expect(select).toHaveValue(''), { timeout: 2000 })
  })
})
