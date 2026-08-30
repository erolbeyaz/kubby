import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type ResourceRow } from '@/lib/api'

import { PodDetail } from './PodDetail'

const pod = {
  metadata: { labels: { app: 'nginx' }, annotations: { 'kubectl.kubernetes.io/restartedAt': '2026-08-24' } },
  spec: {
    nodeName: 'k3d-kubby-test-server-0',
    serviceAccountName: 'default',
    tolerations: [{}, {}],
    volumes: [{ name: 'kube-api-access', projected: {} }],
    containers: [
      {
        name: 'nginx',
        image: 'ghcr.io/nginx/nginx-unprivileged:stable-alpine',
        imagePullPolicy: 'IfNotPresent',
        ports: [{ containerPort: 8080, name: 'http' }],
        env: [{ name: 'A' }, { name: 'B' }],
        resources: { requests: { cpu: '50m', memory: '32Mi' }, limits: { cpu: '250m', memory: '128Mi' } },
        volumeMounts: [{ name: 'kube-api-access', mountPath: '/var/run/secrets/kubernetes.io/serviceaccount', readOnly: true }],
        livenessProbe: {
          httpGet: { path: '/', port: 8080 },
          initialDelaySeconds: 10,
          timeoutSeconds: 2,
          periodSeconds: 10,
        },
      },
    ],
    initContainers: [{ name: 'migrate', image: 'busybox:1.36' }],
  },
  status: {
    podIP: '10.42.2.92',
    qosClass: 'Burstable',
    conditions: [{ type: 'Ready', status: 'True' }, { type: 'PodScheduled', status: 'True' }],
    containerStatuses: [{ name: 'nginx', ready: true, state: { running: {} } }],
    initContainerStatuses: [
      { name: 'migrate', ready: true, state: { terminated: { reason: 'Completed', exitCode: 0 } } },
    ],
  },
}

const row: ResourceRow = {
  name: 'nginx-d6b85b699-8bdwn',
  namespace: 'default',
  age: '46m',
  createdAt: '2026-08-30T11:17:36Z',
  fields: { status: 'Running', controlledBy: 'nginx-d6b85b699', controlledByKind: 'ReplicaSet' },
}

function show(metric: 'cpu' | 'memory' = 'memory') {
  vi.spyOn(api, 'podMetrics').mockResolvedValue({ configured: false })
  vi.spyOn(api, 'resources').mockResolvedValue({ columns: [], rows: [], total: 0, fromCache: false })
  vi.spyOn(api, 'forwards').mockResolvedValue({ forwards: [] })

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <PodDetail
        clusterId="c1"
        typeKey="pods"
        row={row}
        object={pod}
        metric={metric}
        onNavigate={() => {}}
      />
    </QueryClientProvider>,
  )
}

afterEach(() => vi.restoreAllMocks())

describe('PodDetail', () => {
  it('reads the pod out of its own spec and status', async () => {
    show()

    expect(await screen.findByText('10.42.2.92')).toBeInTheDocument()
    expect(screen.getByText('Burstable')).toBeInTheDocument()
    expect(screen.getByText('k3d-kubby-test-server-0')).toBeInTheDocument()
    expect(screen.getByText('ReplicaSet')).toBeInTheDocument()
    // Conditions list only what is true; a pod publishes several that are not.
    expect(screen.getByText('Ready')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  // Init containers ran and exited before anything else started (ADR-134).
  it('keeps init containers in a section of their own', async () => {
    show()

    expect(await screen.findByText('Containers (1)')).toBeInTheDocument()
    expect(screen.getByText('Init Containers (1)')).toBeInTheDocument()
    expect(screen.getByText('Completed, exit 0')).toBeInTheDocument()
  })

  it('says what a container is, is allowed, and is checked with', async () => {
    show()

    expect(await screen.findByText('running, ready')).toBeInTheDocument()
    expect(screen.getByText(/ghcr.io\/nginx\/nginx-unprivileged/)).toBeInTheDocument()
    expect(screen.getByText('cpu 50m · memory 32Mi')).toBeInTheDocument()
    expect(screen.getByText('cpu 250m · memory 128Mi')).toBeInTheDocument()
    // A probe as kubectl describe spells it: what it checks and how patient it is.
    expect(screen.getByText(/http-get http:\/\/:8080\/.*delay=10s.*period=10s/)).toBeInTheDocument()
    expect(screen.getByText('2 variables')).toBeInTheDocument()
    expect(screen.getByText(/serviceaccount/)).toBeInTheDocument()
  })

  // The registry line is ADR-133 and has no equivalent in the layout this follows;
  // dropping it would quietly undo a decision.
  it('still says where the image comes from', async () => {
    show()

    expect(await screen.findByText('ghcr.io')).toBeInTheDocument()
    expect(screen.getByText(/pull IfNotPresent/)).toBeInTheDocument()
  })

  it('says why a chart is missing rather than drawing an empty one', async () => {
    show()
    expect(await screen.findByText(/this cluster has none configured/)).toBeInTheDocument()
  })
})
