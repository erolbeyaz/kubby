import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { NamespacePicker } from './NamespacePicker'

const NAMESPACES = ['default', 'payments', 'storefront']

async function open(selected: string[], onChange = vi.fn()) {
  render(<NamespacePicker namespaces={NAMESPACES} selected={selected} onChange={onChange} />)
  await userEvent.click(screen.getByRole('button', { name: 'Namespace' }))
  return onChange
}

describe('NamespacePicker', () => {
  // An empty selection means every namespace, so the default view should look selected
  // rather than looking like nothing is chosen.
  it('ticks every namespace while all are in scope', async () => {
    await open([])

    expect(screen.getByRole('checkbox', { name: 'All namespaces' })).toBeChecked()
    for (const name of NAMESPACES) {
      expect(screen.getByRole('option', { name })).toHaveAttribute('aria-selected', 'true')
    }
  })

  // Picking a namespace while everything is in scope means "just this one". Reading it
  // as "everything except this one" made reaching a single namespace a chore.
  it('narrows to the one picked while all are in scope', async () => {
    const onChange = await open([])

    await userEvent.click(screen.getByRole('checkbox', { name: 'payments' }))

    expect(onChange).toHaveBeenCalledWith(['payments'])
  })

  it('adds to a narrowed selection', async () => {
    const onChange = await open(['payments'])

    await userEvent.click(screen.getByRole('checkbox', { name: 'default' }))

    expect(onChange).toHaveBeenCalledWith(['payments', 'default'])
  })

  // Nothing selected would show an empty list, which is never what unticking meant.
  it('falls back to all when the last one is unticked', async () => {
    const onChange = await open(['payments'])

    await userEvent.click(screen.getByRole('checkbox', { name: 'payments' }))

    expect(onChange).toHaveBeenCalledWith([])
  })

  // Ticking the last one back is the same view as "all", and keeping it as a list would
  // freeze the set and hide namespaces created later.
  it('collapses a full selection back to all', async () => {
    const onChange = await open(['default', 'payments'])

    await userEvent.click(screen.getByRole('checkbox', { name: 'storefront' }))

    expect(onChange).toHaveBeenCalledWith([])
  })

  it('returns to all namespaces from a narrowed selection', async () => {
    const onChange = await open(['payments'])

    expect(screen.getByRole('checkbox', { name: 'All namespaces' })).not.toBeChecked()
    await userEvent.click(screen.getByRole('checkbox', { name: 'All namespaces' }))

    expect(onChange).toHaveBeenCalledWith([])
  })
})
