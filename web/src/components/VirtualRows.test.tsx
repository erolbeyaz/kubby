import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { VirtualRows } from './VirtualRows'

describe('VirtualRows', () => {
  // A namespace with thousands of pods is ordinary; mounting every row is what makes
  // such a list unusable long before the data does.
  it('mounts only a window of rows, not the whole list', () => {
    const items = Array.from({ length: 5000 }, (_, i) => `row-${i}`)

    render(
      <div style={{ height: 300 }}>
        <VirtualRows items={items} rowHeight={30}>
          {(item) => <div key={item}>{item}</div>}
        </VirtualRows>
      </div>,
    )

    const rendered = screen.queryAllByText(/^row-\d+$/)
    expect(rendered.length).toBeGreaterThan(0)
    expect(rendered.length).toBeLessThan(200)
  })

  it('reserves the full scroll height so the scrollbar is honest', () => {
    const items = Array.from({ length: 100 }, (_, i) => `row-${i}`)

    const { container } = render(
      <VirtualRows items={items} rowHeight={30}>
        {(item) => <div key={item}>{item}</div>}
      </VirtualRows>,
    )

    const spacer = container.querySelector('[style*="height: 3000px"]')
    expect(spacer).not.toBeNull()
  })
})
