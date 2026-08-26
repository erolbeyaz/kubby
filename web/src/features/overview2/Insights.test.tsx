import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { NodeDetail } from '@/lib/api'

import { NodeConditions, Placement } from './Insights'

function node(over: Partial<NodeDetail> = {}): NodeDetail {
  return {
    name: 'worker-01',
    role: 'worker',
    ready: true,
    cpuPercent: 20,
    memoryPercent: 30,
    diskPercent: 10,
    cores: 4,
    memoryTotalBytes: 16e9,
    cpuCommittedPercent: 10,
    memoryCommittedPercent: 10,
    pods: 10,
    podCapacity: 110,
    networkRxBytes: 0,
    networkTxBytes: 0,
    loadPerCore: 0.4,
    uptimeSeconds: 86400,
    memoryPressure: false,
    diskPressure: false,
    pidPressure: false,
    networkUnavailable: false,
    unschedulable: false,
    swapPercent: 0,
    swapTotalBytes: 0,
    inodePercent: 5,
    diskReadBytes: 0,
    diskWriteBytes: 0,
    diskBusyPercent: 0,
    ioWaitPercent: 0,
    networkRxErrors: 0,
    networkTxErrors: 0,
    networkDrops: 0,
    clockSkewSeconds: 0,
    bootTimeUnix: 0,
    nodeExporterUp: true,
    kubeletUp: true,
    cpuLimitPercent: 0,
    memoryLimitPercent: 0,
    cpuAllocatable: 4,
    memoryAllocatable: 16e9,
    ...over,
  }
}

describe('NodeConditions', () => {
  // The panel this replaced was a wall of green squares on a healthy cluster. Silence
  // should cost one line, not a third of the screen.
  it('collapses to a single sentence when nothing is raised', () => {
    render(<NodeConditions issues={[]} nodes={[node(), node({ name: 'worker-02' })]} />)

    expect(screen.getByText(/All 2 nodes are Ready/i)).toBeInTheDocument()
    expect(screen.queryByText('MemoryPressure')).not.toBeInTheDocument()
  })

  // The fact the old grid threw away: a condition raised ninety seconds ago is a rollout,
  // the same condition raised two days ago is a full disk.
  it('names the condition and how long it has been true', () => {
    render(
      <NodeConditions
        issues={[{ node: 'worker-06', condition: 'DiskPressure', minutes: 2880 }]}
        nodes={[node({ name: 'worker-06' })]}
      />,
    )

    expect(screen.getByText('DiskPressure')).toBeInTheDocument()
    expect(screen.getByText(/for 2d/)).toBeInTheDocument()
    // And what it means, rather than only its name.
    expect(screen.getByText(/running out of disk/i)).toBeInTheDocument()
  })

  it('reports a cordoned node and one nothing is scraping', () => {
    render(
      <NodeConditions
        issues={[]}
        nodes={[
          node({ name: 'worker-03', unschedulable: true }),
          node({ name: 'worker-04', nodeExporterUp: false }),
        ]}
      />,
    )

    expect(screen.getByText('SchedulingDisabled')).toBeInTheDocument()
    expect(screen.getByText('ExporterNotScraped')).toBeInTheDocument()
    expect(screen.getByText(/may be stale/i)).toBeInTheDocument()
  })

  it('puts a not-ready node above everything else', () => {
    render(
      <NodeConditions
        issues={[]}
        nodes={[node({ name: 'worker-09', ready: false })]}
      />,
    )

    expect(screen.getByText('NotReady')).toBeInTheDocument()
    expect(screen.getByText(/0 of 1 nodes are clear/)).toBeInTheDocument()
  })
})

describe('Placement', () => {
  const nodes = ['worker-01', 'worker-02']

  // The question the heatmap could not answer without hovering over every cell.
  it('names the namespaces that would lose everything with one node', () => {
    render(
      <Placement
        spread={[
          { namespace: 'payments', node: 'worker-01', pods: 6 },
          { namespace: 'shop', node: 'worker-01', pods: 2 },
          { namespace: 'shop', node: 'worker-02', pods: 3 },
        ]}
        nodes={nodes}
      />,
    )

    expect(screen.getByText(/1 namespace would lose every pod/i)).toBeInTheDocument()
    expect(screen.getByText(/all 6 pods on worker-01/i)).toBeInTheDocument()
    expect(screen.getByText(/5 pods across 2 nodes/i)).toBeInTheDocument()
  })

  it('says so plainly when everything is spread', () => {
    render(
      <Placement
        spread={[
          { namespace: 'shop', node: 'worker-01', pods: 2 },
          { namespace: 'shop', node: 'worker-02', pods: 2 },
        ]}
        nodes={nodes}
      />,
    )

    expect(screen.getByText(/spread across at least two/i)).toBeInTheDocument()
  })

  // A single-pod namespace is on one node by definition; calling that a risk would cry
  // wolf on every CronJob in the cluster.
  it('does not flag a namespace that only has one pod', () => {
    render(
      <Placement spread={[{ namespace: 'cron', node: 'worker-01', pods: 1 }]} nodes={nodes} />,
    )

    expect(screen.queryByText(/would lose every pod/i)).not.toBeInTheDocument()
  })

  // The counts used to sit inside the bar segments at nine pixels in the page's own
  // background colour — invisible on a mid-tone fill and clipped away on a narrow one.
  // They belong outside it, in text ink, beside the node they belong to.
  it('reads each count outside the bar, next to the node it belongs to', () => {
    render(
      <Placement
        spread={[
          { namespace: 'shop', node: 'worker-01', pods: 7 },
          { namespace: 'shop', node: 'worker-02', pods: 3 },
        ]}
        nodes={nodes}
      />,
    )

    expect(screen.getByText('7')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    // Named, not only coloured — the node appears against its own count.
    expect(screen.getAllByText('worker-01').length).toBeGreaterThan(0)
    expect(screen.getAllByText('worker-02').length).toBeGreaterThan(0)
  })

  // Colour alone never carries the reading, so the bar says in words what it draws.
  it('describes the split for anyone not looking at the bar', () => {
    render(
      <Placement
        spread={[
          { namespace: 'shop', node: 'worker-01', pods: 7 },
          { namespace: 'shop', node: 'worker-02', pods: 3 },
        ]}
        nodes={nodes}
      />,
    )

    expect(
      screen.getByRole('img', { name: 'worker-01: 7 pods, worker-02: 3 pods' }),
    ).toBeInTheDocument()
  })

  // Colour follows the node, fixed by its place in the cluster's node list. If it
  // followed the row's rank instead, filtering one namespace out would repaint the rest.
  it('gives a node the same colour in every row', () => {
    const { container } = render(
      <Placement
        spread={[
          { namespace: 'shop', node: 'worker-02', pods: 5 },
          { namespace: 'shop', node: 'worker-01', pods: 1 },
          { namespace: 'payments', node: 'worker-01', pods: 4 },
          { namespace: 'payments', node: 'worker-02', pods: 1 },
        ]}
        nodes={nodes}
      />,
    )

    const widest = [...container.querySelectorAll<HTMLElement>('[title]')].filter((segment) =>
      segment.title.startsWith('worker-02'),
    )
    expect(widest.length).toBe(2)
    expect(widest[0]?.style.backgroundColor).toBe(widest[1]?.style.backgroundColor)
  })
})
