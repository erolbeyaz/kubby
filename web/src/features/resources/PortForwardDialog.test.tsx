import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type Forward } from '@/lib/api'

import { PortForwardDialog } from './PortForwardDialog'

function show(port?: number, declared: { port: number; name?: string; protocol: string }[] = []) {
  vi.spyOn(api, 'forwardablePorts').mockResolvedValue({ ports: declared })
  const start = vi.spyOn(api, 'startForward').mockResolvedValue({
    id: 'f1',
    url: 'http://localhost:34849/',
    mode: 'port',
  } as Forward)
  vi.stubGlobal('open', vi.fn())

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <PortForwardDialog
        clusterId="c1"
        typeKey="pods"
        name="nx-seq-1"
        namespace="nx-apps"
        port={port}
        onOpened={() => {}}
        onClose={() => {}}
      />
    </QueryClientProvider>,
  )
  return start
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

// One dialog, whichever way it was reached: the row's menu and a port on the detail
// panel used to ask the same question with different furniture.
describe('PortForwardDialog', () => {
  const fields = ['Port to forward:', 'Local port to forward from:', 'https', 'Open in Browser']

  it('asks the same things when the port is already known', async () => {
    show(80)
    for (const field of fields) {
      expect(await screen.findByText(field)).toBeInTheDocument()
    }
    // Nothing to choose, so the port is simply stated.
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.getByText('80')).toBeInTheDocument()
  })

  it('asks the same things when it is not', async () => {
    show(undefined, [{ port: 8080, name: 'http', protocol: 'TCP' }])
    for (const field of fields) {
      expect(await screen.findByText(field)).toBeInTheDocument()
    }
    await waitFor(() => expect(screen.getByText('8080')).toBeInTheDocument())
  })

  // A pod with several ports has to keep all of them reachable from the row's own menu.
  it('offers a choice only when there is one', async () => {
    show(undefined, [
      { port: 80, name: 'http', protocol: 'TCP' },
      { port: 5341, name: 'ingest', protocol: 'TCP' },
    ])

    const choice = await screen.findByRole('combobox', { name: 'Port to forward' })
    await userEvent.selectOptions(choice, '5341')

    const start = vi.mocked(api.startForward)
    await userEvent.click(screen.getByRole('button', { name: 'Start' }))
    await waitFor(() => expect(start).toHaveBeenCalled())
    expect(start.mock.calls[0]?.[1]).toMatchObject({ port: 5341 })
  })

  it('sends the local port and the scheme it was given', async () => {
    const start = show(80)

    await userEvent.type(screen.getByLabelText('Local port to forward from'), '50215')
    await userEvent.click(screen.getByRole('checkbox', { name: 'https' }))
    await userEvent.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(start).toHaveBeenCalled())
    expect(start.mock.calls[0]?.[1]).toMatchObject({ port: 80, localPort: 50215, https: true })
  })
})
