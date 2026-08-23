import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ResourceRow } from '@/lib/api'

import { ScaleDialog } from './ScaleDialog'

const row = (fields: Record<string, string>): ResourceRow => ({
  name: 'payments-api',
  namespace: 'payments',
  age: '2d',
  createdAt: '2026-08-21T10:00:00Z',
  fields,
})

function open(fields: Record<string, string>, onClose = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <ScaleDialog
        clusterId="c1"
        typeKey="apps/deployments"
        kind="Deployment"
        row={row(fields)}
        onChanged={vi.fn()}
        onClose={onClose}
      />
    </QueryClientProvider>,
  )
  return onClose
}

afterEach(() => vi.unstubAllGlobals())

describe('ScaleDialog', () => {
  it('opens on the count the workload is already asked for', () => {
    open({ ready: '3/5' })

    expect(screen.getByText('Current replica scale: 5')).toBeInTheDocument()
    expect(screen.getByLabelText('Desired number of replicas')).toHaveValue('5')
  })

  it('reads the desired column when there is no ready column', () => {
    open({ desired: '2' })

    expect(screen.getByText('Current replica scale: 2')).toBeInTheDocument()
  })

  // Nothing to do when the answer is the number it already is.
  it('offers nothing to do when the count is unchanged', () => {
    open({ ready: '3/3' })

    expect(screen.getByRole('button', { name: 'Scale' })).toBeDisabled()
  })

  // The question is nearly always "a bit more" or "a bit less", which a slider is
  // clumsy at by one.
  it('steps by one from either side', async () => {
    open({ ready: '3/3' })

    await userEvent.click(screen.getByRole('button', { name: 'One more' }))
    expect(screen.getByLabelText('Desired number of replicas')).toHaveValue('4')

    await userEvent.click(screen.getByRole('button', { name: 'One fewer' }))
    await userEvent.click(screen.getByRole('button', { name: 'One fewer' }))
    expect(screen.getByLabelText('Desired number of replicas')).toHaveValue('2')
  })

  it('will not step below zero', () => {
    open({ ready: '0/0' })

    expect(screen.getByRole('button', { name: 'One fewer' })).toBeDisabled()
  })

  // Zero is a real answer, and it deserves saying what it does rather than a refusal.
  it('warns what zero means', async () => {
    open({ ready: '1/1' })

    await userEvent.click(screen.getByRole('button', { name: 'One fewer' }))

    expect(screen.getByText(/Zero stops every replica/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Scale' })).toBeEnabled()
  })

  it('sends the new count', async () => {
    const fetch = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(() =>
      Promise.resolve(
        new Response(JSON.stringify({ replicas: 4 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    vi.stubGlobal('fetch', fetch)
    const onClose = open({ ready: '3/3' })

    await userEvent.click(screen.getByRole('button', { name: 'One more' }))
    await userEvent.click(screen.getByRole('button', { name: 'Scale' }))

    const body = fetch.mock.calls[0]?.[1]?.body
    expect(typeof body).toBe('string')
    expect(JSON.parse(body as string)).toMatchObject({ replicas: 4, name: 'payments-api' })
    expect(onClose).toHaveBeenCalled()
  })

  // A slider that stops at ten cannot express the change someone opened it to make.
  it('reaches fifty', () => {
    open({ ready: '3/3' })

    expect(screen.getByLabelText('Desired number of replicas')).toHaveAttribute('max', '50')
  })

  // A workload already larger than the ceiling must not be capped below where it started.
  it('goes further for a workload that is already large', () => {
    open({ ready: '40/40' })

    expect(screen.getByLabelText('Desired number of replicas')).toHaveAttribute('max', '80')
  })
})
