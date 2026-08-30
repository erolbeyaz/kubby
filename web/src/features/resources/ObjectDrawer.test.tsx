import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ResourceRow } from '@/lib/api'

import { ObjectDrawer } from './ObjectDrawer'

const POD = {
  metadata: { name: 'api-0', namespace: 'payments', creationTimestamp: '2026-08-21T10:00:00Z' },
  spec: {
    initContainers: [
      {
        name: 'migrate',
        image: 'registry.internal/team/migrate:3.2',
        resources: { requests: { cpu: '5m' } },
      },
    ],
    containers: [
      {
        name: 'api',
        image: 'team/api:1.4.0',
        imagePullPolicy: 'IfNotPresent',
        ports: [{ containerPort: 8080, name: 'http' }],
        resources: { requests: { cpu: '10m', memory: '32Mi' }, limits: { memory: '192Mi' } },
        volumeMounts: [{ name: 'config', mountPath: '/etc/api', readOnly: true }],
      },
      { name: 'istio-proxy', image: 'docker.io/istio/proxyv2:1.22.0' },
    ],
    volumes: [{ name: 'config', configMap: { name: 'api-config' } }],
  },
  status: {
    podIP: '10.42.5.71',
    containerStatuses: [
      { name: 'api', ready: true, state: { running: {} }, imageID: 'docker.io/team/api@sha256:abc' },
      { name: 'istio-proxy', ready: true, state: { running: {} }, imageID: '' },
    ],
    initContainerStatuses: [
      { name: 'migrate', ready: false, state: { terminated: { reason: 'Completed', exitCode: 0 } } },
    ],
  },
}

function mockApi(object: Record<string, unknown>) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      const body = url.includes('/relations/')
        ? { relations: [] }
        : url.includes('/restarts')
          ? { app: 0, sidecar: 0, init: 0, details: [] }
          : object

      return Promise.resolve(
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }),
  )
}

function renderDrawer(kind = 'Pod') {
  const row: ResourceRow = {
    name: 'api-0',
    namespace: 'payments',
    age: '2d',
    createdAt: '2026-08-21T10:00:00Z',
    fields: { status: 'Running', restarts: '0', cpu: '12m', memory: '40Mi' },
  }

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ObjectDrawer
        clusterId="c1"
        typeKey="pods"
        kind={kind}
        row={row}
        onClose={() => {}}
        onNavigate={() => {}}
        onAction={() => {}}
      />
    </QueryClientProvider>,
  )
}

afterEach(() => vi.unstubAllGlobals())

describe('ObjectDrawer containers', () => {
  it('labels the image and says which registry it resolves to', async () => {
    mockApi(POD)
    renderDrawer()

    expect(await screen.findByText('team/api:1.4.0')).toBeInTheDocument()
    // The reference names no host, so where it comes from has to be said out loud.
    expect(screen.getAllByText('Image').length).toBeGreaterThan(0)
    expect(screen.getAllByText('docker.io').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/\(implicit\)/).length).toBeGreaterThan(0)
    expect(screen.getByText('docker.io/team/api@sha256:abc')).toBeInTheDocument()
  })

  // An init container ran and exited before anything else started; reading it among the
  // containers still running invites treating the two as the same thing.
  it('keeps init containers in a section of their own', async () => {
    mockApi(POD)
    renderDrawer()

    expect(await screen.findByText('Containers (2)')).toBeInTheDocument()
    expect(screen.getByText('Init Containers (1)')).toBeInTheDocument()
    expect(screen.getByText('registry.internal/team/migrate:3.2')).toBeInTheDocument()
  })

  it('shows what each container asked for, listens on and mounts', async () => {
    mockApi(POD)
    renderDrawer()

    expect(await screen.findByText('cpu 10m · memory 32Mi')).toBeInTheDocument()
    expect(screen.getByText('memory 192Mi')).toBeInTheDocument()
    // A sidecar with no limits at all is the one worth noticing, so it says so.
    expect(screen.getAllByText('none').length).toBeGreaterThan(0)
    expect(screen.getByText('8080/TCP http')).toBeInTheDocument()
    expect(screen.getByText(/\/etc\/api/)).toBeInTheDocument()
  })

  it('lists the volumes the pod carries and what backs them', async () => {
    mockApi(POD)
    renderDrawer()

    expect(await screen.findByText('Volumes (1)')).toBeInTheDocument()
    expect(screen.getByText('ConfigMap')).toBeInTheDocument()
    expect(screen.getByText('api-config')).toBeInTheDocument()
  })

  // The phases it promised have shipped; a panel that still promises them is stale.
  it('no longer promises what later phases will bring', async () => {
    mockApi(POD)
    renderDrawer()

    await screen.findByText('team/api:1.4.0')
    expect(screen.queryByText(/Coming next/i)).not.toBeInTheDocument()
  })

  // A workload has no containers of its own; what it will run is in its template.
  it('reads a deployment through the pod template it stamps out', async () => {
    mockApi({
      metadata: { name: 'api', namespace: 'payments' },
      spec: { template: { spec: POD.spec } },
      status: {},
    })
    renderDrawer('Deployment')

    expect(await screen.findByText('team/api:1.4.0')).toBeInTheDocument()
    expect(screen.getByText('Init Containers (1)')).toBeInTheDocument()
  })

  // The name is what gets carried into a terminal or a ticket, and it is truncated in
  // the header, so selecting it by hand gets half of it.
  it('offers the name for copying', async () => {
    mockApi(POD)
    renderDrawer()

    expect(await screen.findByRole('button', { name: 'Copy' })).toBeInTheDocument()
  })
})
