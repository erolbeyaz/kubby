import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { SecretField } from './SecretField'

function open(stored: boolean, onClear = vi.fn(), onChange = vi.fn()) {
  render(
    <SecretField
      label="Password"
      stored={stored}
      value=""
      clear={false}
      onChange={onChange}
      onClear={onClear}
    />,
  )
  return { onClear, onChange }
}

describe('SecretField', () => {
  // A form that wipes a password because the field was not retyped is a form that loses
  // passwords, and whoever saved it has no way to know until something stops working.
  it('says an empty field keeps what is stored', () => {
    open(true)

    expect(screen.getByText(/Leave this empty to keep it/)).toBeInTheDocument()
  })

  it('says plainly when nothing is stored', () => {
    open(false)

    expect(screen.getByText('Nothing is stored yet.')).toBeInTheDocument()
    expect(screen.queryByText(/Remove the stored/)).not.toBeInTheDocument()
  })

  // Removing is a separate act from replacing, so it needs its own control.
  it('offers removal only when there is something to remove', async () => {
    const { onClear } = open(true)

    await userEvent.click(screen.getByRole('checkbox', { name: /Remove the stored password/ }))

    expect(onClear).toHaveBeenCalledWith(true)
  })

  it('never shows the stored value', () => {
    open(true)

    const field = screen.getByLabelText('Password')
    expect(field).toHaveAttribute('type', 'password')
    expect(field).toHaveValue('')
  })
})
