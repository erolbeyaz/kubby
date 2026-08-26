import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { StatusBar } from './StatusBar'

afterEach(() => document.documentElement.setAttribute('data-theme', 'dark'))

describe('StatusBar', () => {
  it('carries the author signature', () => {
    render(<StatusBar connection="ready" version={undefined} />)

    expect(screen.getByAltText('powered by erolbeyaz')).toBeInTheDocument()
  })

  // Centred on the strip rather than in the flow, so a long status message on the left
  // does not push it off centre.
  it('centres the signature independently of the status text', () => {
    render(<StatusBar connection="degraded" version={undefined} detail="a very long status message" />)

    const slot = screen.getByAltText('powered by erolbeyaz').parentElement
    expect(slot?.className).toContain('absolute')
    expect(slot?.className).toContain('left-1/2')
  })

  // The monogram that used to appear above the signature on hover was removed: the
  // signature already says who made this, and a second mark on the same three pixels
  // was one flourish too many.
  it('carries no monogram', () => {
    render(<StatusBar connection="ready" version={undefined} />)

    const mark = screen.getByAltText('powered by erolbeyaz')
    expect(mark.parentElement?.querySelector('.monogram')).toBeNull()
  })
})
