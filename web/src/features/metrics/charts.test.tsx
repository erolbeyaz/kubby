import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { AreaChart, Donut, format, formatBytes } from './charts'

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

describe('AreaChart', () => {
  const at = (hour: number, minute = 0) =>
    `2026-08-26T${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}:00Z`

  const series = (points: Array<{ at: string; value: number }>) => [
    { name: 'node-a', points, colour: 'var(--accent)' },
  ]

  // A line with no axis says "something changed" without saying when, which is the half
  // that separates a rollout from an outage.
  it('writes the window it covers under the plot', () => {
    render(
      <AreaChart
        series={series([
          { at: at(7), value: 10 },
          { at: at(9), value: 20 },
          { at: at(11), value: 15 },
        ])}
        unit="%"
      />,
    )

    // UTC on the wire, Europe/Istanbul on the screen (ADR-026): 07:00Z reads as 10:00.
    const axis = screen.getByTitle(/times in Europe\/Istanbul/)
    expect(axis).toHaveTextContent('10:00')
    expect(axis).toHaveTextContent('12:00')
    expect(axis).toHaveTextContent('14:00')
  })

  // 22:00, 02:00, 06:00 leaves the reader working out which of those is yesterday.
  it('adds the date once the window crosses one', () => {
    render(
      <AreaChart
        series={series([
          { at: '2026-08-25T06:00:00Z', value: 1 },
          { at: '2026-08-25T18:00:00Z', value: 2 },
          { at: '2026-08-26T06:00:00Z', value: 3 },
        ])}
      />,
    )

    const axis = screen.getByTitle(/times in Europe\/Istanbul/)
    expect(axis).toHaveTextContent('25 Aug')
    expect(axis).toHaveTextContent('26 Aug')
  })

  // Bytes per second read as bytes per second, not as a bare "4k".
  it('lets a chart write its own unit', () => {
    render(
      <AreaChart
        series={series([
          { at: at(7), value: 1024 },
          { at: at(8), value: 4096 },
        ])}
        render={(value) => `${formatBytes(value)}/s`}
      />,
    )

    // min, max and current all wear the unit.
    expect(screen.getByText('1 KiB/s')).toBeInTheDocument()
    expect(screen.getAllByText('4 KiB/s').length).toBe(2)
  })

  // A chart with nothing to draw says so rather than drawing a flat line at zero.
  it('says when there is not enough history to draw', () => {
    render(<AreaChart series={series([{ at: at(7), value: 1 }])} />)
    expect(screen.getByText('not enough history yet')).toBeInTheDocument()
  })
})
