import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CopyButton } from './CopyButton'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('CopyButton', () => {
  it('copies the value and says so', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    render(<CopyButton value="kind: Pod" label="Copy YAML" />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy YAML' }))

    expect(writeText).toHaveBeenCalledWith('kind: Pod')
    expect(await screen.findByText('Copied')).toBeInTheDocument()
  })

  // An insecure origin or a denied permission rejects the async clipboard. Silent
  // failure is indistinguishable from a broken button.
  it('reports a refused clipboard', async () => {
    vi.stubGlobal('navigator', {
      ...navigator,
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) },
    })
    vi.spyOn(document, 'execCommand').mockReturnValue(false)
    render(<CopyButton value="kind: Pod" />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy' }))

    expect(await screen.findByText('Copy failed')).toBeInTheDocument()
  })

  it('falls back to the legacy copy when the clipboard is refused', async () => {
    vi.stubGlobal('navigator', {
      ...navigator,
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) },
    })
    const legacy = vi.spyOn(document, 'execCommand').mockReturnValue(true)
    render(<CopyButton value="kind: Pod" />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy' }))

    expect(legacy).toHaveBeenCalledWith('copy')
    expect(await screen.findByText('Copied')).toBeInTheDocument()
  })
})
