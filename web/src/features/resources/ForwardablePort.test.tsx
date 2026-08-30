import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type Forward } from '@/lib/api'

import { ForwardablePort } from './ForwardablePort'

function show(port = 80, protocol = 'TCP') {
  vi.spyOn(api, 'forwards').mockResolvedValue({ forwards: [] })
  const start = vi.spyOn(api, 'startForward').mockResolvedValue({
    id: 'f1',
    url: 'http://localhost:34849/',
    mode: 'port',
  } as Forward)
  const open = vi.fn()
  vi.stubGlobal('open', open)

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ForwardablePort
        clusterId="c1"
        typeKey="pods"
        namespace="nx-apps"
        name="nx-seq-1"
        port={port}
        protocol={protocol}
        label="http"
      />
    </QueryClientProvider>,
  )
  return { start, open }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('ForwardablePort', () => {
  it('shows the port the way the rest of the panel does', () => {
    show()
    expect(screen.getByRole('button', { name: /Forward 80/ })).toHaveTextContent('80/TCP http')
  })

  // The number is where the reader already is when they wonder what is listening on it.
  it('forwards and opens the tab from the click itself', async () => {
    const { start, open } = show()

    await userEvent.click(screen.getByRole('button', { name: /Forward 80/ }))

    await waitFor(() => expect(start).toHaveBeenCalled())
    expect(start.mock.calls[0]?.[1]).toMatchObject({ type: 'pods', namespace: 'nx-apps', port: 80 })
    expect(open).toHaveBeenCalledWith('http://localhost:34849/', '_blank', 'noopener')
  })

  // A forward carries a stream, and UDP is not one.
  it('leaves a UDP port as text', () => {
    show(53, 'UDP')
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.getByText('53/UDP http')).toBeInTheDocument()
  })

  it('says so when the cluster refuses', async () => {
    vi.spyOn(api, 'forwards').mockResolvedValue({ forwards: [] })
    const start = vi.spyOn(api, 'startForward').mockRejectedValue(new Error('nope'))
    vi.stubGlobal('open', vi.fn())

    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ForwardablePort
          clusterId="c1"
          typeKey="services"
          namespace="nx-apps"
          name="nx-seq"
          port={80}
          protocol="TCP"
        />
      </QueryClientProvider>,
    )

    await userEvent.click(screen.getByRole('button', { name: /Forward 80/ }))
    await waitFor(() => expect(start).toHaveBeenCalled())
    expect(await screen.findByText('failed')).toBeInTheDocument()
  })
})

describe('a port that is already forwarded', () => {
  // Once a tunnel exists the only thing left to do with it is end it, and the reader
  // should not have to go looking for where.
  it('offers Stop instead of Forward, and opens the address it already has', async () => {
    vi.spyOn(api, 'forwards').mockResolvedValue({
      forwards: [
        {
          id: 'f1',
          namespace: 'nx-apps',
          name: 'nx-seq-1',
          port: 80,
          url: 'http://localhost:34849/',
          mode: 'port',
        } as Forward,
      ],
    })
    const start = vi.spyOn(api, 'startForward')
    const stop = vi.spyOn(api, 'stopForward').mockResolvedValue(undefined)
    const open = vi.fn()
    vi.stubGlobal('open', open)

    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ForwardablePort
          clusterId="c1"
          typeKey="pods"
          namespace="nx-apps"
          name="nx-seq-1"
          port={80}
          protocol="TCP"
          label="http"
        />
      </QueryClientProvider>,
    )

    expect(await screen.findByRole('button', { name: 'Stop' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Forward…' })).not.toBeInTheDocument()

    // The address is the one already open, not a second tunnel to the same port.
    await userEvent.click(screen.getByRole('button', { name: /Open 80/ }))
    expect(open).toHaveBeenCalledWith('http://localhost:34849/', '_blank', 'noopener')
    expect(start).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(stop).toHaveBeenCalledWith('f1'))
  })
})
