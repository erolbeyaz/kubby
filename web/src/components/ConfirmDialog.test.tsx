import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ConfirmDialog } from './ConfirmDialog'

function open(onConfirm = vi.fn(), onCancel = vi.fn()) {
  render(
    <ConfirmDialog
      destructive
      title="Delete Deployment payments-api"
      phrase="payments-api"
      confirmLabel="Delete"
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <p>This cannot be undone.</p>
    </ConfirmDialog>,
  )
  return { onConfirm, onCancel }
}

describe('ConfirmDialog', () => {
  // The phrase is the only thing that makes the reader look at what they selected; a
  // confirm button that works without it is a button people learn to click through.
  it('keeps the action locked until the phrase matches', async () => {
    const { onConfirm } = open()

    const confirm = screen.getByRole('button', { name: 'Delete' })
    expect(confirm).toBeDisabled()

    await userEvent.type(screen.getByLabelText('Confirmation'), 'payments')
    expect(confirm).toBeDisabled()

    await userEvent.type(screen.getByLabelText('Confirmation'), '-api')
    expect(confirm).toBeEnabled()

    await userEvent.click(confirm)
    expect(onConfirm).toHaveBeenCalled()
  })

  it('does not accept a near miss', async () => {
    open()

    await userEvent.type(screen.getByLabelText('Confirmation'), 'payments-API')

    expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled()
  })

  it('closes on Escape', async () => {
    const { onCancel } = open()

    await userEvent.keyboard('{Escape}')

    expect(onCancel).toHaveBeenCalled()
  })

  it('closes on Cancel without acting', async () => {
    const { onConfirm, onCancel } = open()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(onCancel).toHaveBeenCalled()
    expect(onConfirm).not.toHaveBeenCalled()
  })

  // Several objects are confirmed by reading the list, not by typing a phrase in front
  // of it: the list is what has to be looked at.
  it('asks plainly when there is no phrase', async () => {
    const onConfirm = vi.fn()
    render(
      <ConfirmDialog
        destructive
        title="Delete 2 Pod"
        confirmLabel="Delete"
        onConfirm={onConfirm}
        onCancel={vi.fn()}
      >
        <p>Delete 2 Pod?</p>
      </ConfirmDialog>,
    )

    expect(screen.queryByLabelText('Confirmation')).not.toBeInTheDocument()

    const confirm = screen.getByRole('button', { name: 'Delete' })
    expect(confirm).toBeEnabled()

    await userEvent.click(confirm)
    expect(onConfirm).toHaveBeenCalled()
  })
})
