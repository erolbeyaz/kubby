import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ResourceRow } from '@/lib/api'

import { DeleteDialog } from './DeleteDialog'

const row = (name: string, fields: Record<string, string> = {}): ResourceRow => ({
  name,
  namespace: 'payments',
  age: '2d',
  createdAt: '2026-08-21T10:00:00Z',
  fields,
})

function open(rows: ResourceRow[], onClose = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <DeleteDialog
        target={{ clusterId: 'c1', typeKey: 'pods', kind: 'Pod', rows }}
        onChanged={vi.fn()}
        onClose={onClose}
      />
    </QueryClientProvider>,
  )
  return onClose
}

afterEach(() => vi.unstubAllGlobals())

describe('DeleteDialog', () => {
  it('asks plainly, without a phrase to retype', () => {
    open([row('payments-api-1')])

    expect(screen.queryByLabelText('Confirmation')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeEnabled()
  })

  // "3 objects" is a number someone can agree to without reading; the names are the
  // thing the gate exists to make them look at.
  it('lists every object by name', () => {
    open([row('a'), row('b'), row('c')])

    for (const name of ['a', 'b', 'c']) {
      expect(screen.getByText(`payments/${name}`)).toBeInTheDocument()
    }
  })

  // The reported confusion: a controller replaces the pod within a second, so the delete
  // reads as having failed. Saying so first turns a non-event into a decision.
  it('warns that a controller will replace what is deleted', () => {
    open([row('payments-api-1', { controlledBy: 'payments-api-6d5c', controlledByKind: 'ReplicaSet' })])

    expect(screen.getByText(/controller will start a replacement/)).toBeInTheDocument()
    expect(screen.getByText(/ReplicaSet payments-api-6d5c/)).toBeInTheDocument()
  })

  it('says nothing about controllers when nothing is managed', () => {
    open([row('standalone')])

    expect(screen.queryByText(/controller/)).not.toBeInTheDocument()
  })

  // One blanket failure would hide which of them are still there.
  it('reports failures against the object that failed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = input instanceof Request ? input.url : input.toString()
        const failing = url.includes('name=b')
        return Promise.resolve(
          new Response(JSON.stringify(failing ? { error: 'still in use' } : { deleted: true }), {
            status: failing ? 409 : 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        )
      }),
    )
    const onClose = open([row('a'), row('b')])

    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))

    expect(await screen.findByText(/still in use/)).toBeInTheDocument()
    // The dialog stays open: closing it would hide the one that did not go.
    expect(onClose).not.toHaveBeenCalled()
  })
})
