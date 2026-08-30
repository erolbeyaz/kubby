import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError, api, type ResourceRow } from '@/lib/api'

import { BulkScaleDialog } from './BulkScaleDialog'

function row(name: string, ready: string, scaledFrom?: string): ResourceRow {
  return {
    name,
    namespace: 'payments',
    age: '1d',
    createdAt: '',
    fields: { ready, ...(scaledFrom === undefined ? {} : { scaledFrom }) },
  }
}

function show(rows = [row('api', '3/3'), row('worker', '5/5')]) {
  const scale = vi.spyOn(api, 'scale').mockResolvedValue({ replicas: 0 })
  const closed = vi.fn()

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <BulkScaleDialog
        clusterId="c1"
        typeKey="apps/deployments"
        kind="Deployment"
        rows={rows}
        onChanged={() => {}}
        onClose={closed}
      />
    </QueryClientProvider>,
  )
  return { scale, closed }
}

afterEach(() => vi.restoreAllMocks())

describe('BulkScaleDialog', () => {
  // What is about to be flattened. A drill that takes 3 and 5 to zero and comes back as
  // 1 and 1 is a worse outage than the one it was rehearsing.
  it('shows what each workload runs now, and warns when they differ', () => {
    show()

    expect(screen.getByText('payments/api')).toBeInTheDocument()
    expect(screen.getByText('now 3')).toBeInTheDocument()
    expect(screen.getByText('now 5')).toBeInTheDocument()
    expect(screen.getByText(/do not all run the same count/)).toBeInTheDocument()
  })

  it('scales every selected workload to the number given', async () => {
    const { scale, closed } = show()

    await userEvent.click(screen.getByRole('button', { name: 'Scale to zero' }))

    await waitFor(() => expect(scale).toHaveBeenCalledTimes(2))
    expect(scale.mock.calls[0]?.[1]).toMatchObject({ name: 'api', replicas: 0 })
    expect(scale.mock.calls[1]?.[1]).toMatchObject({ name: 'worker', replicas: 0 })
    await waitFor(() => expect(closed).toHaveBeenCalled())
  })

  // Bringing a set back is not one number: they did not all run the same count, and
  // Kubernetes keeps no record of what they ran.
  it('restores each workload to its own recorded count', async () => {
    const { scale } = show([row('api', '0/0', '3'), row('worker', '0/0', '5')])

    await userEvent.click(screen.getByRole('button', { name: 'Restore previous' }))
    await userEvent.click(screen.getByRole('button', { name: 'Restore' }))

    await waitFor(() => expect(scale).toHaveBeenCalledTimes(2))
    expect(scale.mock.calls[0]?.[1]).toMatchObject({ name: 'api', restore: true })
  })

  // A failure part-way must not lose the ones that worked, or the reader re-runs the
  // whole thing without knowing what already happened.
  it('names what was refused and keeps the dialog open', async () => {
    const scale = vi
      .spyOn(api, 'scale')
      .mockResolvedValueOnce({ replicas: 0 })
      .mockRejectedValueOnce(new ApiError('is owned by ArgoCD', 409, 'r1'))
    const closed = vi.fn()

    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <BulkScaleDialog
          clusterId="c1"
          typeKey="apps/deployments"
          kind="Deployment"
          rows={[row('api', '3/3'), row('worker', '5/5')]}
          onChanged={() => {}}
          onClose={closed}
        />
      </QueryClientProvider>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Scale to zero' }))

    await waitFor(() => expect(scale).toHaveBeenCalledTimes(2))
    expect(await screen.findByText(/payments\/worker: is owned by ArgoCD/)).toBeInTheDocument()
    expect(closed).not.toHaveBeenCalled()
  })
})

describe('BulkScaleDialog, restoring', () => {
  // "now 0" on every row says nothing about where they are going. The whole reason to
  // press Restore is the number on the other side of the arrow.
  it('shows each workload its own recorded count as the target', async () => {
    show([row('api', '0/0', '3'), row('worker', '0/0', '5')])

    await userEvent.click(screen.getByRole('button', { name: 'Restore previous' }))

    expect(screen.getByText('→ 3')).toBeInTheDocument()
    expect(screen.getByText('→ 5')).toBeInTheDocument()
  })

  it('marks a workload Kubby never scaled instead of inventing a count for it', async () => {
    show([row('api', '0/0', '3'), row('worker', '2/2')])

    await userEvent.click(screen.getByRole('button', { name: 'Restore previous' }))

    expect(screen.getByText('no record')).toBeInTheDocument()
    expect(screen.getByText(/1 of 2 were not scaled from here/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restore' })).toBeEnabled()
  })

  it('will not run a restore when nothing in the selection has a record', async () => {
    show([row('api', '2/2'), row('worker', '2/2')])

    await userEvent.click(screen.getByRole('button', { name: 'Restore previous' }))

    expect(screen.getByRole('button', { name: 'Restore' })).toBeDisabled()
  })

  it('shows the target in set mode too', () => {
    show([row('api', '3/3'), row('worker', '5/5')])

    expect(screen.getAllByText('→ 0')).toHaveLength(2)
  })
})
