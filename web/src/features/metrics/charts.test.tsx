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

/** An hour of samples, one a minute, starting on an awkward second. */
function anHour(): { at: string; value: number }[] {
  const start = Date.parse('2026-08-30T13:07:23Z')
  return Array.from({ length: 61 }, (_, minute) => ({
    at: new Date(start + minute * 60_000).toISOString(),
    value: minute,
  }))
}

describe('AreaChart time axis', () => {
  // The default is what a small panel has room for: the ends bound the window and the
  // middle gives it a scale (ADR-129).
  it('carries three labels when it is not told otherwise', () => {
    render(<AreaChart series={[{ name: 'cpu', colour: '#0af', points: anHour() }]} />)

    const axis = screen.getByTitle(/times in/)
    expect(axis.children).toHaveLength(3)
  })

  // Labels a reader can predict — :10, :20, :30 — are read at a glance; ones landing on
  // 13:07 and 13:19 have to be worked out.
  it('snaps to a round interval when asked for more', () => {
    render(<AreaChart series={[{ name: 'cpu', colour: '#0af', points: anHour() }]} ticks={7} />)

    const labels = Array.from(screen.getByTitle(/times in/).children).map((node) => node.textContent)
    expect(labels.length).toBeGreaterThan(3)
    for (const label of labels) {
      expect(label).toMatch(/[0-9]{2}:(00|10|20|30|40|50)/)
    }
  })

  // Read by finding a value between two labels, which two gaps do not give much room
  // for — and off by default, because on a small panel the scale costs the line width.
  it('draws the value scale only when asked', () => {
    const { container, rerender } = render(
      <AreaChart series={[{ name: 'cpu', colour: '#0af', points: anHour() }]} unit="%" />,
    )
    expect(screen.queryByText('45%')).not.toBeInTheDocument()

    rerender(<AreaChart series={[{ name: 'cpu', colour: '#0af', points: anHour() }]} unit="%" values />)

    // Round steps rather than quarters of the highest sample: 49.5% and 16.5% are
    // correct and no use, and a scale is read by counting between two labels.
    expect(scaleOf(container)).toEqual(['60%', '40%', '20%', '0%'])
  })

  // The reason for rounding: a node idling at 0.02 cores needs a scale that goes to
  // 0.025 in steps of 0.005, not one that goes to 0.022 in steps of 0.0055.
  it('picks a step a reader can count in, whatever the size of the numbers', () => {
    const tiny = Array.from({ length: 5 }, (_, i) => ({ at: `${i}`, value: 0.02 }))
    const { container } = render(
      <AreaChart series={[{ name: 'cpu', colour: '#0af', points: tiny }]} values render={(v) => v.toFixed(3)} />,
    )

    expect(scaleOf(container)).toEqual(['0.020', '0.015', '0.010', '0.005', '0.000'])
  })
})

/** The column of values down the right edge, which is deliberately hidden from the
 *  accessibility tree — the legend below carries the same numbers with names on them. */
function scaleOf(container: HTMLElement): string[] {
  const scale = container.querySelector('[aria-hidden="true"].absolute')
  return Array.from(scale?.children ?? []).map((node) => node.textContent ?? '')
}
