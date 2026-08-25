import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type KubbySettings } from '@/lib/api'

import { AuditSinkSection } from './AuditSinkSection'

const off: KubbySettings['auditSink'] = {
  enabled: false,
  kind: 'elasticsearch',
  url: '',
  hasToken: false,
  insecureSkipVerify: false,
  dataStream: false,
}

function show(value: KubbySettings['auditSink'] = off) {
  const save = vi.spyOn(api, 'saveAuditSink').mockResolvedValue({} as KubbySettings)

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <AuditSinkSection value={value} />
    </QueryClientProvider>,
  )
  return save
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AuditSinkSection', () => {
  // Shipping that is configured but switched off looks identical to shipping that is
  // broken: the endpoint is filled in, the screen looks ready, and nothing arrives. This
  // asserts the switch actually reaches the request.
  it('sends the switch state with the rest of the form', async () => {
    const save = show()

    await userEvent.click(screen.getByLabelText('Ship the audit trail'))
    await userEvent.type(screen.getByLabelText('Endpoint'), 'http://localhost:9200')
    await userEvent.type(screen.getByLabelText('Index or stream'), 'kubby-audit')
    await userEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      enabled: true,
      kind: 'elasticsearch',
      url: 'http://localhost:9200',
      index: 'kubby-audit',
    })
  })

  it('carries the destination when it is changed', async () => {
    const save = show({ ...off, enabled: true, url: 'http://localhost:9200' })

    await userEvent.selectOptions(screen.getByLabelText('Destination'), 'loki')
    await userEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0]?.[0]).toMatchObject({ kind: 'loki', enabled: true })
  })

  // Turning shipping off is a deliberate act and must reach the server as one, rather
  // than being lost because only "on" is treated as a change.
  it('sends the switch being turned off', async () => {
    const save = show({ ...off, enabled: true, url: 'http://localhost:9200' })

    await userEvent.click(screen.getByLabelText('Ship the audit trail'))
    await userEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0]?.[0]).toMatchObject({ enabled: false })
  })

  // Only the kinds that have a sender behind them. Syslog was offered here and had none,
  // so choosing it left the screen saying shipping was on while nothing left the process.
  // A data stream is not a naming convention: it changes the bulk verb and needs an index
  // template, so the switch has to reach the server rather than being cosmetic.
  it('sends the data stream choice', async () => {
    const save = show({ ...off, enabled: true, url: 'http://localhost:9200' })

    await userEvent.click(screen.getByLabelText('Write to a data stream'))
    await userEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0]?.[0]).toMatchObject({ dataStream: true })
  })

  // Only Elasticsearch has data streams; offering the switch for Loki would be a control
  // that does nothing.
  it('offers the data stream switch only for Elasticsearch', async () => {
    show({ ...off, enabled: true, url: 'http://localhost:3100' })

    expect(screen.getByLabelText('Write to a data stream')).toBeInTheDocument()
    await userEvent.selectOptions(screen.getByLabelText('Destination'), 'loki')
    expect(screen.queryByLabelText('Write to a data stream')).not.toBeInTheDocument()
  })

  it('offers only destinations that can actually ship', () => {
    show()

    const options = Array.from(
      screen.getByLabelText('Destination').querySelectorAll('option'),
    ).map((option) => option.getAttribute('value'))

    expect(options).toEqual(['elasticsearch', 'loki', 'http'])
  })
})
