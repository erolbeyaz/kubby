import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Cluster, Me } from '@/lib/api'

import { ManageClustersScreen } from './ManageClustersScreen'

const ADMIN = { permissions: ['cluster.read', 'cluster.manage'] } as Me
const VIEWER = { permissions: ['cluster.read'] } as Me

const base = {
  environmentLabel: '',
  color: '',
  authSource: 'kubeconfig',
  insecureSkipTlsVerify: false,
  metricsAvailable: true,
  readOnly: false,
  impersonationEnabled: false,
  qpsLimit: 50,
  metricsInsecureSkipVerify: false,
  logsInsecureSkipVerify: false,
}

const CLUSTERS = [
  {
    ...base, id: 'c1', name: 'kubby-mini', environment: 'test', displayEnvironment: 'test',
    apiServerUrl: 'https://192.168.58.2:8443', credentialStatus: 'valid',
    k8sVersion: 'v1.34.4', nodeCount: 3, lastValidatedAt: '2026-08-26T10:00:00Z',
  },
  {
    ...base, id: 'c2', name: 'prod-app', environment: 'prod', displayEnvironment: 'Production',
    apiServerUrl: 'https://prod-app.internal:6443', credentialStatus: 'valid',
    k8sVersion: 'v1.33.2', nodeCount: 8, lastValidatedAt: '2026-08-26T10:00:00Z',
  },
  {
    ...base, id: 'c3', name: 'prod-rancher', environment: 'prod', displayEnvironment: 'Production',
    apiServerUrl: 'https://prod-rancher.internal/k8s/clusters/c-m-x',
    credentialStatus: 'unreachable', statusDetail: 'dial tcp: no such host',
  },
  {
    ...base, id: 'c4', name: 'dr-site', environment: 'dr', displayEnvironment: 'DR',
    apiServerUrl: 'https://dr.internal:6443', credentialStatus: 'unknown',
  },
] as unknown as Cluster[]

function renderScreen(me: Me = ADMIN, clusters: Cluster[] = CLUSTERS) {
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

  const onOpenCluster = vi.fn()
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ManageClustersScreen me={me} onOpenCluster={onOpenCluster} />
    </QueryClientProvider>,
  )
  return { ...view, onOpenCluster }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('the cluster registry', () => {
  // Environment is the one fact here that changes what the tool will let you do, and
  // "is anything in production broken" is the question people arrive with. Sorting by
  // name puts "dr" above "prod" and answers a question nobody asked.
  it('groups by environment with production first', async () => {
    const { container } = renderScreen()

    await waitFor(() => expect(screen.getByText('kubby-mini')).toBeInTheDocument())

    const tiers = [...container.querySelectorAll('section')].map(
      (section) => section.textContent?.match(/^(Production|Pre-production|Test|Disaster recovery)/)?.[0],
    )
    expect(tiers).toEqual(['Production', 'Test', 'Disaster recovery'])
  })

  // A status column is read row by row: on a fleet of eight, the one that stopped
  // answering is one line among eight that look alike.
  it('names what is broken above what exists, with the reason', async () => {
    renderScreen()

    const banner = (await screen.findByText(/2 clusters need attention/)).closest('div') as HTMLElement
    expect(within(banner).getByText('dial tcp: no such host')).toBeInTheDocument()
    expect(within(banner).getByText('never checked')).toBeInTheDocument()
    // Both broken clusters are named, so neither has to be hunted for in the list.
    expect(within(banner).getByText('prod-rancher')).toBeInTheDocument()
    expect(within(banner).getByText('dr-site')).toBeInTheDocument()
  })

  it('says nothing about attention when nothing needs it', async () => {
    renderScreen(ADMIN, [CLUSTERS[0] as Cluster])

    await waitFor(() => expect(screen.getByText('kubby-mini')).toBeInTheDocument())
    expect(screen.queryByText(/needs? attention/)).not.toBeInTheDocument()
  })

  // "Down" is a claim, and a cluster nobody has checked is not one Kubby can make.
  it('counts a never-checked cluster as unchecked, not as down', async () => {
    const { container } = renderScreen()

    await waitFor(() => expect(screen.getByText('kubby-mini')).toBeInTheDocument())

    const dr = [...container.querySelectorAll('section')].find((section) =>
      section.textContent?.startsWith('Disaster recovery'),
    )
    expect(dr?.textContent).toContain('1 unchecked')
    expect(dr?.textContent).not.toContain('down')

    const prod = [...container.querySelectorAll('section')].find((section) =>
      section.textContent?.startsWith('Production'),
    )
    expect(prod?.textContent).toContain('1 down')
  })

  // The row is a place to go, not a decision to make. Three buttons on every line made
  // the list read as a form.
  it('opens a cluster from its row', async () => {
    const { onOpenCluster, container } = renderScreen()

    await waitFor(() => expect(screen.getByText('kubby-mini')).toBeInTheDocument())
    const test = [...container.querySelectorAll('section')].find((section) =>
      section.textContent?.startsWith('Test'),
    )
    await userEvent.click(within(test as HTMLElement).getByText('kubby-mini'))

    expect(onOpenCluster).toHaveBeenCalledWith('c1')
  })

  it('offers no way to add a cluster to someone who cannot manage them', async () => {
    renderScreen(VIEWER)

    await waitFor(() => expect(screen.getByText('kubby-mini')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /Connect a cluster/ })).not.toBeInTheDocument()
  })

  // An empty screen is an invitation to act, not a notice that there is nothing here.
  it('invites the first cluster rather than reporting an absence', async () => {
    renderScreen(ADMIN, [])

    await waitFor(() => expect(screen.getByText('Connect your first cluster')).toBeInTheDocument())
    await userEvent.click(screen.getAllByRole('button', { name: /Connect a cluster/ })[0] as HTMLElement)

    // Straight into the flow, on a page of its own.
    expect(await screen.findByText('Name it')).toBeInTheDocument()
  })
})
