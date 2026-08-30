import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type Forward } from '@/lib/api'

import { PortForwards } from './PortForwards'

function forward(over: Partial<Forward> = {}): Forward {
  return {
    id: 'f1',
    clusterId: 'c1',
    type: 'pods',
    namespace: 'kube-system',
    name: 'metrics-server-786d997795-rb785',
    pod: 'metrics-server-786d997795-rb785',
    port: 10250,
    url: 'https://localhost:54866/',
    startedAt: new Date(Date.now() - 5 * 60_000).toISOString(),
    mode: 'port',
    localPort: 54866,
    protocol: 'https',
    kind: 'pod',
    ...over,
  }
}

function show(forwards: Forward[]) {
  vi.spyOn(api, 'forwards').mockResolvedValue({ forwards })
  const stop = vi.spyOn(api, 'stopForward').mockResolvedValue(undefined)

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <PortForwards clusterId="c1" />
    </QueryClientProvider>,
  )
  return stop
}

afterEach(() => vi.restoreAllMocks())

describe('PortForwards', () => {
  // A tunnel exists only while Kubby holds it, so it appears in no other list. Until
  // this screen the only way to find one was to remember which pod it came from.
  it('lists what each tunnel points at and how to reach it', async () => {
    show([forward()])

    expect(await screen.findByText('metrics-server-786d997795-rb785')).toBeInTheDocument()
    expect(screen.getByText('kube-system')).toBeInTheDocument()
    expect(screen.getByText('pod')).toBeInTheDocument()
    expect(screen.getByText('10250')).toBeInTheDocument()
    expect(screen.getByText('54866')).toBeInTheDocument()
    expect(screen.getByText('https')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
  })

  it('says plainly when nothing is forwarded', async () => {
    show([])
    expect(await screen.findByText('Nothing is forwarded')).toBeInTheDocument()
  })

  it('stops a tunnel from its own row', async () => {
    const stop = show([forward()])

    await userEvent.click(await screen.findByRole('button', { name: /Actions for metrics-server/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Stop' }))

    await waitFor(() => expect(stop).toHaveBeenCalledWith('f1'))
  })

  // A proxied tunnel behaves differently enough that the row has to say so rather than
  // let the reader find out by the page misbehaving.
  it('marks a tunnel that fell back to the proxy', async () => {
    show([forward({ mode: 'proxy', localPort: undefined, url: '/api/v1/forward/f1/' })])

    expect(await screen.findByText('proxied')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})
