import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { LogFinding } from '@/lib/api'

import { LogFindingTable } from './tables'

const now = Date.parse('2026-08-30T12:00:00Z')

function finding(over: Partial<LogFinding> = {}): LogFinding {
  return {
    namespace: 'nx-apps',
    pod: 'api-0',
    rule: 'SQL Server',
    class: 'auth',
    count: 40,
    sample: 'Cannot open database "Orders" requested by the login.',
    firstSeen: new Date(now - 5 * 60_000).toISOString(),
    lastSeen: new Date(now).toISOString(),
    severity: 'error',
    ...over,
  }
}

describe('LogFindingTable', () => {
  it('says so plainly when nothing is reporting', () => {
    render(<LogFindingTable rows={[]} />)
    expect(screen.getByText(/No pod is reporting an error/)).toBeInTheDocument()
  })

  // The number that decides whether to stop and look now is how long it has been true,
  // not how loud it is: a pod writing 500 lines a second for a minute is noisier than
  // one that has been quietly failing since yesterday.
  it('puts the longest-running failure first, however quiet it is', () => {
    render(
      <LogFindingTable
        rows={[
          finding({ pod: 'loud', count: 500_000, firstSeen: new Date(now - 60_000).toISOString() }),
          finding({
            pod: 'old',
            count: 12,
            firstSeen: new Date(now - 22 * 3600_000).toISOString(),
          }),
        ]}
      />,
    )

    const rows = screen.getAllByRole('row').slice(1)
    expect(rows[0]!).toHaveTextContent('old')
    expect(rows[0]!).toHaveTextContent('22h 0m')
    expect(rows[1]!).toHaveTextContent('loud')
  })

  it('reads a duration in the units the reader thinks in', () => {
    render(
      <LogFindingTable
        rows={[
          finding({ pod: 'a', firstSeen: new Date(now - 30_000).toISOString() }),
          finding({ pod: 'b', firstSeen: new Date(now - 45 * 60_000).toISOString() }),
          finding({ pod: 'c', firstSeen: new Date(now - 3 * 24 * 3600_000).toISOString() }),
        ]}
      />,
    )

    expect(screen.getByText('under a minute')).toBeInTheDocument()
    expect(screen.getByText('45m')).toBeInTheDocument()
    expect(screen.getByText('3d 0h')).toBeInTheDocument()
  })

  // Nine replicas saying the same thing is the same news nine times.
  it('says how many other pods share a rolled-up finding', () => {
    render(<LogFindingTable rows={[finding({ pods: 9 })]} />)
    expect(screen.getByText('and 8 more pods')).toBeInTheDocument()
  })

  it('shows the extracted identity above the raw line', () => {
    render(<LogFindingTable rows={[finding({ summary: 'database Orders · user svc-orders' })]} />)

    expect(screen.getByText('database Orders · user svc-orders')).toBeInTheDocument()
    expect(screen.getByText(/Cannot open database/)).toBeInTheDocument()
  })
})
