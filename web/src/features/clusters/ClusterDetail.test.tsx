import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/lib/api'

import { ClusterDetail } from './ClusterDetail'

const CLUSTER = {
  id: 'c1',
  name: 'prod-app',
  environment: 'prod',
  environmentLabel: 'Production',
  displayEnvironment: 'Production',
  color: '',
  authSource: 'kubeconfig',
  apiServerUrl: 'https://prod-app.internal:6443',
  insecureSkipTlsVerify: false,
  credentialStatus: 'invalid',
  statusDetail: 'the token has expired',
  k8sVersion: 'v1.33.2',
  nodeCount: 8,
  metricsAvailable: true,
  readOnly: true,
  impersonationEnabled: false,
  qpsLimit: 50,
  metricsInsecureSkipVerify: false,
} as unknown as Cluster

function renderDetail(cluster: Cluster = CLUSTER, canManage = true) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      const json = (body: unknown) =>
        Promise.resolve(
          new Response(JSON.stringify(body), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        )
      if (url.includes('/grants')) return json({ grants: [] })
      if (url.includes('/users')) return json({ users: [] })
      return json({})
    }),
  )

  const onBack = vi.fn()
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ClusterDetail cluster={cluster} canManage={canManage} onBack={onBack} />
    </QueryClientProvider>,
  )
  return { ...view, onBack }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('cluster detail', () => {
  // The rail is a summary as well as a route: what is wrong should be visible before
  // choosing where to go, not after arriving.
  it('says what is in each section on the rail', async () => {
    const { container } = renderDetail()

    const rail = container.querySelector('nav') as HTMLElement
    await waitFor(() => expect(within(rail).getByText('Connection')).toBeInTheDocument())

    expect(rail.textContent).toContain('not connected')
    expect(rail.textContent).toContain('Production')
    expect(rail.textContent).toContain('deployment default')
    expect(rail.textContent).toContain('locked read-only')
  })

  // Seven panels in one scroll put the delete button at the bottom of everything.
  it('keeps removing a cluster somewhere you have to mean to arrive', async () => {
    renderDetail()

    expect(screen.queryByText('Remove cluster')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Remove/ }))
    expect(await screen.findByText('Remove cluster')).toBeInTheDocument()

    // Production still demands the name typed out (ADR: a confirm dialog is clicked
    // through by muscle memory).
    expect(screen.getByLabelText(/Type “prod-app” to confirm/)).toBeInTheDocument()
  })

  it('opens on the connection, and says why it is not connected', async () => {
    renderDetail()

    expect(await screen.findByText("This cluster's credential no longer works")).toBeInTheDocument()
    expect(screen.getByText('the token has expired')).toBeInTheDocument()
  })

  // A rail with one entry on it is a rail pretending there is somewhere else to go.
  it('shows no rail to someone who cannot manage the cluster', async () => {
    const { container } = renderDetail(CLUSTER, false)

    await waitFor(() => expect(screen.getAllByText('prod-app').length).toBeGreaterThan(0))
    expect(container.querySelector('nav')).toBeNull()
    expect(screen.queryByRole('button', { name: /Remove/ })).not.toBeInTheDocument()
  })
})
