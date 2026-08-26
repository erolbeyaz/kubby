import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Button } from './Button'

describe('Button', () => {
  // The bug this guards against: the variant's colours were written as inline
  // `background-color`, which always beats a stylesheet `:hover` — so the CSS rule never
  // fired and the button did nothing under the pointer. Handing them in as custom
  // properties is what lets `.btn:hover` win.
  it('hands its colours to CSS rather than setting them inline', () => {
    render(<Button variant="primary">Save</Button>)

    const button = screen.getByRole('button', { name: 'Save' })
    expect(button.className).toContain('btn')
    expect(button.style.backgroundColor).toBe('')
    expect(button.style.getPropertyValue('--btn-bg')).toBe('var(--accent)')
    expect(button.style.getPropertyValue('--btn-bg-hover')).toBe('var(--accent-hover)')
  })

  it('gives every variant somewhere to go on hover', () => {
    for (const variant of ['primary', 'secondary', 'danger'] as const) {
      const { unmount } = render(<Button variant={variant}>{variant}</Button>)
      const button = screen.getByRole('button', { name: variant })

      for (const property of ['--btn-bg-hover', '--btn-border-hover', '--btn-fg-hover']) {
        expect(button.style.getPropertyValue(property), `${variant} ${property}`).not.toBe('')
      }
      unmount()
    }
  })

  it('is not clickable while it is working', () => {
    render(<Button loading>Testing</Button>)
    expect(screen.getByRole('button', { name: 'Testing' })).toBeDisabled()
  })
})
