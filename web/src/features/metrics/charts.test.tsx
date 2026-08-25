import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Donut, format, formatBytes } from './charts'

describe('Donut', () => {
  // Each slice is drawn as one long dash rotated into place. If the rotations are
  // computed from a value mutated while rendering, the ring depends on render order and
  // React may legitimately give a different one.
  it('starts each slice where the previous one ended', () => {
    const { container } = render(
      <Donut
        slices={[
          { name: 'Running', value: 75, colour: 'green' },
          { name: 'Pending', value: 15, colour: 'orange' },
          { name: 'Failed', value: 10, colour: 'red' },
        ]}
      />,
    )

    const rotations = Array.from(container.querySelectorAll('circle')).map((circle) =>
      Number(/rotate\((-?[\d.]+)/.exec(circle.getAttribute('transform') ?? '')?.[1]),
    )

    // Twelve o'clock, then three quarters round, then fifteen percent further.
    expect(rotations).toEqual([-90, 180, 234])
  })

  it('puts the total in the hole', () => {
    render(
      <Donut
        slices={[
          { name: 'Running', value: 26, colour: 'green' },
          { name: 'Failed', value: 2, colour: 'red' },
        ]}
        centreLabel="pods"
      />,
    )

    expect(screen.getByText('28')).toBeInTheDocument()
    expect(screen.getByText('pods')).toBeInTheDocument()
  })

  it('says so rather than dividing by zero', () => {
    render(<Donut slices={[]} />)
    expect(screen.getByText('nothing to show')).toBeInTheDocument()
  })
})

describe('number formatting', () => {
  it('keeps small values readable rather than rounding them to nothing', () => {
    // A namespace using two thousandths of a core must not read as "0".
    expect(format(8.2)).toBe('8.2')
    expect(format(0.5)).toBe('0.5')
    expect(format(12)).toBe('12')
    expect(format(1500)).toBe('1.5k')
  })

  it('scales bytes to the unit a person would say out loud', () => {
    expect(formatBytes(418004992)).toBe('399 MiB')
    expect(formatBytes(2 * 1024 * 1024 * 1024)).toBe('2.0 GiB')
    expect(formatBytes(512)).toBe('512 B')
  })
})
