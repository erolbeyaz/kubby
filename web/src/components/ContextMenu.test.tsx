import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ContextMenu, type MenuItem } from './ContextMenu'

const items: MenuItem[] = [
  { id: 'details', label: 'Show details', onSelect: vi.fn() },
  { id: 'logs', label: 'Logs', onSelect: vi.fn() },
  { id: 'shell', label: 'Shell', disabled: true, note: 'phase 8' },
  { id: 'delete', label: 'Delete', destructive: true, onSelect: vi.fn() },
]

function open(onClose = vi.fn()) {
  render(<ContextMenu items={items} at={{ x: 10, y: 10 }} onClose={onClose} />)
  return onClose
}

describe('ContextMenu', () => {
  // Destructive entries sit below a divider, away from an accidental click.
  it('puts destructive entries last', () => {
    open()

    const labels = screen.getAllByRole('menuitem').map((item) => item.textContent)
    expect(labels?.[labels.length - 1]).toContain('Delete')
  })

  // The shape of what an object can do is part of understanding it, so entries a later
  // phase brings are listed and disabled rather than hidden.
  it('shows unavailable actions with the phase that brings them', () => {
    open()

    const shell = screen.getByRole('menuitem', { name: /Shell/ })
    expect(shell).toBeDisabled()
    expect(shell).toHaveTextContent('phase 8')
  })

  // Delete works, so it must not be sitting behind a "coming later" label.
  it('offers delete as a real action', async () => {
    const onClose = open()

    const remove = screen.getByRole('menuitem', { name: /Delete/ })
    expect(remove).toBeEnabled()

    await userEvent.click(remove)
    expect(items[3]?.onSelect).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })

  it('runs the chosen action and closes', async () => {
    const onClose = open()

    await userEvent.click(screen.getByRole('menuitem', { name: 'Logs' }))

    expect(items[1]?.onSelect).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })

  it('closes on Escape', async () => {
    const onClose = open()

    await userEvent.keyboard('{Escape}')

    expect(onClose).toHaveBeenCalled()
  })
})
