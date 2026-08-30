import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type ResourceRow } from '@/lib/api'

import { NodeDetail } from './NodeDetail'

const node = {
  metadata: {
    labels: { a: '1', b: '2' },
    annotations: { x: '1' },
    finalizers: ['wrangler.cattle.io/node'],
  },
  status: {
    addresses: [
      { type: 'InternalIP', address: '172.20.0.2' },
      { type: 'Hostname', address: 'k3d-kubby-test-server-0' },
    ],
    capacity: { cpu: '4', memory: '16000000Ki', 'ephemeral-storage': '1006900000Ki', pods: '110' },
    allocatable: { cpu: '4', memory: '16000000Ki', 'ephemeral-storage': '956500000Ki', pods: '110' },
    conditions: [
      { type: 'MemoryPressure', status: 'False' },
      { type: 'Ready', status: 'True', message: 'kubelet is posting ready status' },
    ],
    nodeInfo: {
      operatingSystem: 'linux',
      architecture: 'amd64',
      osImage: 'K3s v1.35.5+k3s1',
      kernelVersion: '6.18.33.2-microsoft-standard-WSL2',
      containerRuntimeVersion: 'containerd://2.2.3-k3s1',
      kubeletVersion: 'v1.35.5+k3s1',
    },
  },
}

const row: ResourceRow = {
  name: 'k3d-kubby-test-server-0',
  age: '7d',
  createdAt: '2026-08-22T19:30:01Z',
  fields: {},
}

function show(pods: ResourceRow[] = []) {
  vi.spyOn(api, 'clusterMetrics').mockResolvedValue({ configured: false })
  vi.spyOn(api, 'resources').mockImplementation((_cluster, typeKey) =>
    Promise.resolve({
      columns: [],
      rows: typeKey === 'pods' ? pods : [],
      total: 0,
      fromCache: false,
    }),
  )

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <NodeDetail clusterId="c1" row={row} object={node} onNavigate={() => {}} />
    </QueryClientProvider>,
  )
}

afterEach(() => vi.restoreAllMocks())

describe('NodeDetail', () => {
  it('reads the machine out of its own status', async () => {
    show()

    expect(await screen.findByText('K3s v1.35.5+k3s1')).toBeInTheDocument()
    expect(screen.getByText('6.18.33.2-microsoft-standard-WSL2')).toBeInTheDocument()
    expect(screen.getByText('containerd://2.2.3-k3s1')).toBeInTheDocument()
    expect(screen.getByText('linux (amd64)')).toBeInTheDocument()
    expect(screen.getByText('InternalIP: 172.20.0.2')).toBeInTheDocument()
    expect(screen.getByText('2 Labels')).toBeInTheDocument()
    expect(screen.getByText('1 Annotation')).toBeInTheDocument()
    expect(screen.getByText('wrangler.cattle.io/node')).toBeInTheDocument()
  })

  // A node raises every condition it knows about and sets most of them to False.
  // Listing those is a wall of things that are fine.
  it('shows only the conditions that are raised', async () => {
    show()

    expect(await screen.findByText('Ready')).toBeInTheDocument()
    expect(screen.queryByText('MemoryPressure')).not.toBeInTheDocument()
  })

  // The gap between the two is what the kubelet reserved, which is the whole reason
  // both are shown.
  it('shows capacity beside allocatable, in units a reader thinks in', async () => {
    show()

    expect(await screen.findByText('Capacity')).toBeInTheDocument()
    expect(screen.getByText('Allocatable')).toBeInTheDocument()
    // 1006900000Ki and 956500000Ki are the same to the eye until they are converted.
    expect(screen.getByText('960.3 GiB')).toBeInTheDocument()
    expect(screen.getByText('912.2 GiB')).toBeInTheDocument()
  })

  it('lists only the pods that landed on this node', async () => {
    show([
      { name: 'here', namespace: 'payments', age: '1d', createdAt: '', fields: { node: row.name, status: 'Running' } },
      { name: 'elsewhere', namespace: 'payments', age: '1d', createdAt: '', fields: { node: 'other-node' } },
    ])

    expect(await screen.findByText('here')).toBeInTheDocument()
    expect(screen.queryByText('elsewhere')).not.toBeInTheDocument()
    expect(screen.getByText('Pods (1)')).toBeInTheDocument()
  })

  // Metrics come from Prometheus. A cluster without one must say so rather than draw an
  // empty chart that reads as a quiet machine.
  it('says why the chart is empty rather than drawing nothing', async () => {
    show()
    expect(await screen.findByText(/No history for this node/)).toBeInTheDocument()
  })
})
