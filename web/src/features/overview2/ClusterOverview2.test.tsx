import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/lib/api'

import { ClusterOverview2 } from './ClusterOverview2'

const known = (value: number) => ({ value, known: true })
const unknown = { value: 0, known: false }

const cluster = {
  id: 'c1',
  name: 'prod-eu',
  k8sVersion: 'v1.33.2',
  environment: 'prod',
  displayEnvironment: 'Production',
  color: '',
  apiServerUrl: 'https://prod-eu.k8s.internal:6443',
} as Cluster

const summary = {
  status: 'Degraded' as const,
  reasons: ['2 workload(s) short of replicas'],
  nodesReady: known(7),
  nodesTotal: known(8),
  nodesNotReady: known(1),
  nodesUnschedulable: known(0),
  nodesUnderPressure: known(1),
  podsReady: known(487),
  podsTotal: known(492),
  podsPending: known(3),
  longestPendingSeconds: known(720),
  containersNotStarting: known(2),
  restarts1h: known(14),
  oomKilled: known(2),
  evicted: unknown,
  unavailableWorkloads: known(1),
  alertsCritical: unknown,
  alertsWarning: unknown,
  apiErrorRate: known(0.12),
  targetsDown: known(2),
  targetsTotal: known(216),
}

const at = (minute: number) => `2026-08-26T10:${String(minute).padStart(2, '0')}:00Z`

/** Three nodes, both directions — the shape that used to draw six lines. */
const NETWORK_TRENDS = {
  networkRx: [
    { name: 'node-a', points: [{ at: at(0), value: 1000 }, { at: at(1), value: 2000 }] },
    { name: 'node-b', points: [{ at: at(0), value: 500 }, { at: at(1), value: 1000 }] },
    { name: 'node-c', points: [{ at: at(0), value: 500 }, { at: at(1), value: 1000 }] },
  ],
  networkTx: [
    { name: 'node-a', points: [{ at: at(0), value: 100 }, { at: at(1), value: 200 }] },
    { name: 'node-b', points: [{ at: at(0), value: 100 }, { at: at(1), value: 200 }] },
    { name: 'node-c', points: [{ at: at(0), value: 100 }, { at: at(1), value: 200 }] },
  ],
  diskByNode: [],
  cpuByNodeOverTime: [],
  memoryByNodeOverTime: [],
  ioWaitByNode: [],
  sparks: {},
}

function metricsBody(overrides: Record<string, unknown> = {}) {
  return {
    configured: true,
    source: 'auto',
    endpoint: 'monitoring/prometheus-server',
    health: {
      pods: { running: 487, pending: 3, failed: 2, succeeded: 0, unknown: 0 },
      nodes: { ready: 7, total: 8 },
      restarts24h: 40,
      windowMinutes: 360,
      summary,
      podProblems: [
        {
          namespace: 'payments',
          pod: 'api-7b88f',
          container: 'app',
          node: 'worker-03',
          status: 'CrashLoopBackOff',
          reason: 'container will not start',
          severity: 'error',
          restarts: 11,
          ageSeconds: 1620,
          cpuUsed: known(0.62),
          cpuRequest: known(0.5),
          cpuLimit: known(1),
          memoryUsed: known(954204160),
          memoryRequest: known(805306368),
          memoryLimit: known(1073741824),
        },
        {
          namespace: 'data',
          pod: 'reporting-0',
          node: 'unscheduled',
          status: 'Pending',
          reason: 'insufficient memory',
          severity: 'warn',
          restarts: 0,
          ageSeconds: 720,
          cpuUsed: unknown,
          cpuRequest: known(2),
          cpuLimit: known(4),
          memoryUsed: unknown,
          memoryRequest: known(8589934592),
          memoryLimit: known(12884901888),
        },
      ],
      ...overrides,
    },
  }
}

// A whole ResourceList, not a bare { rows }: the client validates the shape and drops
// the entire response if one field is missing — which is how the counts this screen
// reads arrived empty while the events panel looked fine.
const EMPTY_EVENTS = { columns: [], rows: [], total: 0, fromCache: false }

const WORKLOAD_COUNTS = [
  { kind: 'Pod', typeKey: 'pods', total: 23, ready: 23 },
  { kind: 'Deployment', typeKey: 'deployments', total: 5, ready: 4 },
  { kind: 'CronJob', typeKey: 'cronjobs', total: 0, ready: 0 },
]

function respond(body: unknown, status = 200) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      // The inventory strip and the events panel share a second endpoint.
      const payload = url.includes('workloads-overview')
        ? { counts: WORKLOAD_COUNTS, events: EMPTY_EVENTS }
        : body
      return Promise.resolve(
        new Response(JSON.stringify(payload), {
          status: url.includes('workloads-overview') ? 200 : status,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }),
  )
}

function renderScreen(onOpenObject = vi.fn(), onOpenType = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <ClusterOverview2 cluster={cluster} onOpenObject={onOpenObject} onOpenType={onOpenType} />
    </QueryClientProvider>,
  )
  return { onOpenObject, onOpenType }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Overview 2', () => {
  // Every number on this screen belongs to one cluster; reading them against the wrong
  // one is the expensive mistake, so the name and its environment lead the header.
  it('names the cluster it is showing', async () => {
    respond(metricsBody())
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText('prod-eu')).toBeInTheDocument()
    })
    expect(screen.getByText('Production')).toBeInTheDocument()
    expect(
      screen.getByText(/Cluster overview · v1\.33\.2 · https:\/\/prod-eu\.k8s\.internal:6443/),
    ).toBeInTheDocument()
  })

  // A pod whose image will not pull is in phase Pending too. Counting it as pending put
  // a number on this tile that no row in the table underneath accounted for.
  it('separates pods waiting to be placed from containers that will not start', async () => {
    respond(metricsBody())
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText(/3 pending/)).toBeInTheDocument()
    })
    expect(screen.getByText(/2 will not start/)).toBeInTheDocument()
  })

  // What the cluster is made of, under its name — and every tile opens its list.
  it('shows what the cluster is made of and opens each kind', async () => {
    respond(metricsBody())
    const { onOpenType } = renderScreen()

    const tile = await screen.findByTitle('4 of 5 Deployments ready')
    expect(tile).toHaveTextContent('5')
    expect(tile).toHaveTextContent('4 ready')

    await userEvent.click(tile)
    expect(onOpenType).toHaveBeenCalledWith('deployments')
  })

  // "23 / 23" on every tile is a column of noise that hides the one tile where the two
  // numbers differ.
  it('does not repeat the total as a ready count when nothing is missing', async () => {
    respond(metricsBody())
    renderScreen()

    const tile = await screen.findByTitle('23 of 23 Pods ready')
    expect(tile).not.toHaveTextContent('ready')
  })

  // It used to draw one line per node per direction, every one of them labelled with the
  // node's name — six lines and a legend naming each machine twice with no way to tell
  // which row was which direction. It was also the only panel in that row with twice the
  // legend rows of its neighbours, which is what made it the tall one.
  it('draws network as two lines, not two per node', async () => {
    respond(metricsBody({ trends: NETWORK_TRENDS }))
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText('received')).toBeInTheDocument()
    })
    expect(screen.getByText('transmitted')).toBeInTheDocument()
    // The node names are gone from this panel: they belong to the node table.
    expect(screen.queryByText('node-a')).not.toBeInTheDocument()
  })

  // Bytes per second read as bytes per second, not as a bare "4k".
  it('gives the network numbers their unit', async () => {
    respond(metricsBody({ trends: NETWORK_TRENDS }))
    renderScreen()

    // 2000 + 1000 + 1000 received at the last sample, which is also its peak.
    await waitFor(() => {
      expect(screen.getAllByText('4 KiB/s').length).toBeGreaterThan(0)
    })
    // And the transmitted total beside it, rather than a bare "600".
    expect(screen.getAllByText('600 B/s').length).toBeGreaterThan(0)
  })

  it('leads with the verdict and the conditions behind it', async () => {
    respond(metricsBody())
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText(/Cluster status: Degraded/)).toBeInTheDocument()
    })
    // The reason is the point: "Degraded" alone sends somebody hunting.
    expect(screen.getByText(/2 workload\(s\) short of replicas/)).toBeInTheDocument()
  })

  // The rule that carries over from every other screen here.
  it('shows N/A for a metric nobody collects, not a reassuring zero', async () => {
    respond(metricsBody())
    renderScreen()

    await waitFor(() => {
      expect(screen.getAllByText('N/A').length).toBeGreaterThan(0)
    })
    expect(screen.getAllByText('not collected').length).toBeGreaterThan(0)
  })

  it('opens the objects behind a headline number', async () => {
    respond(metricsBody())
    const { onOpenType } = renderScreen()

    const tile = await screen.findByRole('button', { name: /Node health/ })
    await userEvent.click(tile)
    expect(onOpenType).toHaveBeenCalledWith('nodes')
  })

  // A tile Kubby could not read must not offer a link: the list behind it proves nothing.
  it('does not link from a tile it could not read', async () => {
    respond(metricsBody())
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText(/Cluster status/)).toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: /Active alerts/ })).not.toBeInTheDocument()
  })

  it('opens the object a problem row names', async () => {
    respond(metricsBody())
    const { onOpenObject } = renderScreen()

    const row = await screen.findByText('payments/api-7b88f')
    await userEvent.click(row)
    expect(onOpenObject).toHaveBeenCalledWith('Pod', 'payments', 'api-7b88f')
  })

  // The brief's table, not a compressed list: the reader is comparing rows down a column.
  it('shows problem pods as a table with usage against requests and limits', async () => {
    respond(metricsBody())
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText('payments/api-7b88f')).toBeInTheDocument()
    })
    expect(screen.getByText('CPU u / r / l')).toBeInTheDocument()
    expect(screen.getByText('Memory u / r / l')).toBeInTheDocument()
    // The node the pod landed on, and the one that never landed.
    expect(screen.getByText('worker-03')).toBeInTheDocument()
    expect(screen.getByText('unscheduled')).toBeInTheDocument()
    // Restarts are coloured only when there are any.
    expect(screen.getByText('11')).toBeInTheDocument()
  })

  // A pending pod has requests and limits but no usage. Zero would claim it is idle;
  // it never started.
  it('writes a dash where a pod has no usage rather than zero', async () => {
    respond(metricsBody())
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText('data/reporting-0')).toBeInTheDocument()
    })
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
  })

  // A failed request must not render as a healthy empty cluster — the bug that made the
  // first dashboard show "everything is healthy" over nothing at all.
  it('says the request failed rather than showing zeros', async () => {
    respond({ error: 'could not read this cluster' }, 500)
    renderScreen()

    await waitFor(() => {
      expect(screen.getAllByText(/could not read this cluster/i).length).toBeGreaterThan(0)
    })
    expect(screen.queryByText(/Cluster status: Healthy/)).not.toBeInTheDocument()
  })

  it('stands down when the cluster has no Prometheus', async () => {
    respond({ configured: false })
    renderScreen()

    await waitFor(() => {
      expect(screen.getByText(/No Prometheus found in this cluster/i)).toBeInTheDocument()
    })
  })
})
