import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { Logo } from './Logo'

function setTheme(value: string | null) {
  if (value === null) {
    document.documentElement.removeAttribute('data-theme')
    return
  }
  document.documentElement.setAttribute('data-theme', value)
}

/** The wordmark is a small SVG, which the bundler inlines as a data URI. */
function decode(image: HTMLElement): string {
  const source = image.getAttribute('src') ?? ''
  const [, encoded = ''] = source.split('base64,')
  return encoded ? atob(encoded) : decodeURIComponent(source)
}

function stubPrefersDark(dark: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: dark && query.includes('dark'),
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }))
}

afterEach(() => {
  setTheme('dark')
  vi.unstubAllGlobals()
})

describe('Logo', () => {
  // A wordmark drawn for dark backgrounds is unreadable on a light one, so the artwork
  // has to follow the theme rather than being picked once.
  it('uses the dark-background wordmark under the dark theme', () => {
    setTheme('dark')
    render(<Logo variant="full" />)

    expect(screen.getByAltText('Kubby').getAttribute('src')).toContain('dark')
  })

  it('uses the light-background wordmark under the light theme', () => {
    setTheme('light')
    render(<Logo variant="full" />)

    expect(screen.getByAltText('Kubby').getAttribute('src')).toContain('light')
  })

  // With no explicit choice the stylesheet follows the system, and so must the artwork.
  it('follows the system preference when no theme is set', () => {
    stubPrefersDark(false)
    setTheme(null)
    render(<Logo variant="full" />)

    expect(screen.getByAltText('Kubby').getAttribute('src')).toContain('light')
  })

  // The header has no room for the artwork's own tagline: at twenty pixels it renders as
  // a smear, so that surface gets the wordmark alone.
  it('uses the tagline-free wordmark in compact places', () => {
    setTheme('dark')
    render(<Logo size={20} variant="wordmark" />)

    const source = decode(screen.getByAltText('Kubby'))
    expect(source).toContain('Kubb')
    expect(source).not.toContain('Simplified')
  })

  // The cube is pure geometry and reads on either background, so it needs no variant.
  it('renders the mark alone at the requested size', () => {
    render(<Logo size={22} />)

    const mark = screen.getByAltText('Kubby')
    expect(mark).toHaveAttribute('width', '22')
    // The cube is vector; the wordmark is not. Small assets are inlined as data URIs,
    // so the assertion is on the format rather than the filename.
    expect(mark.getAttribute('src')).toContain('svg')
  })

  // The artwork's own tagline is 60 units in a 560-unit canvas: at header scale it is a
  // three-pixel smudge. Setting it as text keeps it readable wherever the mark fits.
  it('sets the tagline as text rather than shrinking the artwork', () => {
    render(<Logo size={26} variant="wordmark" tagline />)

    expect(screen.getByText('Kubernetes, Simplified.')).toBeInTheDocument()
    // The tagline is text beside the artwork, not inside it.
    expect(decode(screen.getByAltText('Kubby'))).not.toContain('Simplified')
  })

  it('leaves the tagline out when it is not asked for', () => {
    render(<Logo size={26} variant="wordmark" />)

    expect(screen.queryByText('Kubernetes, Simplified.')).not.toBeInTheDocument()
  })
})
