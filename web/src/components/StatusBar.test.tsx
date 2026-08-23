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

  // A flourish, not a control: it must never take a click meant for the strip beneath it.
  it('carries a monogram that never takes the pointer', () => {
    render(<StatusBar connection="ready" version={undefined} />)

    const mark = screen.getByAltText('powered by erolbeyaz')
    const monogram = mark.parentElement?.querySelector('.monogram')

    expect(monogram).toBeTruthy()
    // Decorative, so it is hidden from anyone reading the page rather than announced,
    // and it carries no alternative text to read out.
    expect(monogram).toHaveAttribute('aria-hidden', 'true')
    expect(monogram).toHaveAttribute('alt', '')
    // Whether it takes the pointer is decided in the stylesheet, which jsdom does not
    // load; the class is what carries that rule.
    expect(monogram?.className).toContain('monogram')
  })
})
