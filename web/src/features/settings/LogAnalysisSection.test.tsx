import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, type KubbySettings } from '@/lib/api'

import { LogAnalysisSection } from './LogAnalysisSection'

const value: KubbySettings['logAnalysis'] = {
  fields: { message: 'log', pod: 'kubernetes.pod_name' },
  rules: [
    { name: 'SQL Server', class: 'auth', match: ['.SqlException', 'Cannot open database'] },
    { name: 'Application error', class: 'generic', match: ['Exception', 'FATAL'] },
  ],
  windowMinutes: 15,
  minCount: 3,
}

function show(over: Partial<KubbySettings['logAnalysis']> = {}) {
  const save = vi.spyOn(api, 'saveLogAnalysis').mockResolvedValue({} as KubbySettings)

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <LogAnalysisSection value={{ ...value, ...over }} />
    </QueryClientProvider>,
  )
  return save
}

afterEach(() => vi.restoreAllMocks())

describe('LogAnalysisSection', () => {
  it('shows the rules that are running and how many of them there are', () => {
    show()

    expect(screen.getByDisplayValue('SQL Server')).toBeInTheDocument()
    // Phrases are edited as one line: a rule is a short list, not a form.
    expect(screen.getByDisplayValue('.SqlException | Cannot open database')).toBeInTheDocument()
    expect(screen.getByText(/2 of 2 rules enabled/)).toBeInTheDocument()
  })

  it('saves an edited phrase list as separate phrases', async () => {
    const save = show()

    const phrases = screen.getByLabelText('Rule 1 phrases')
    await userEvent.clear(phrases)
    await userEvent.type(phrases, 'first | second')
    await userEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0]?.[0].rules[0]?.match).toEqual(['first', 'second'])
  })

  // Deleting a rule loses its wording. Someone silencing a noisy one wants it back later.
  it('can mute a rule without losing it', async () => {
    const save = show()

    await userEvent.click(screen.getAllByRole('checkbox')[1]!)
    expect(screen.getByText(/1 of 2 rules enabled/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /save/i }))
    await waitFor(() => expect(save).toHaveBeenCalled())

    const saved = save.mock.calls[0]?.[0]
    expect(saved?.rules).toHaveLength(2)
    expect(saved?.rules[1]?.disabled).toBe(true)
  })

  it('sends the thresholds as numbers', async () => {
    const save = show()

    const minimum = screen.getByLabelText(/Minimum lines/)
    await userEvent.clear(minimum)
    await userEvent.type(minimum, '10')
    await userEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0]?.[0].minCount).toBe(10)
  })

  it('offers the field names the connection test shows in a document', async () => {
    show()

    await userEvent.click(screen.getByText(/Document fields/))
    expect(screen.getByDisplayValue('kubernetes.pod_name')).toBeInTheDocument()
  })
})
