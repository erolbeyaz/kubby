import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ResizablePanel } from './ResizablePanel'

function widthOf(): number {
  const panel = screen.getByTestId('child').parentElement?.parentElement
  return Number.parseInt(panel?.style.width ?? '0', 10)
}

describe('ResizablePanel', () => {
  // A right-anchored panel grows as the pointer moves left, so the delta is inverted.
  // Sharing the left-anchored maths made the drawer shrink when dragged open.
  it('grows a right-anchored panel when its handle moves left', async () => {
    render(
      <ResizablePanel storageKey="test.right" defaultWidth={400} side="right">
        <div data-testid="child" />
      </ResizablePanel>,
    )

    expect(widthOf()).toBe(400)
    await userEvent.type(screen.getByRole('separator'), '{ArrowLeft}')

    expect(widthOf()).toBe(416)
  })

  it('grows a left-anchored panel when its handle moves right', async () => {
    render(
      <ResizablePanel storageKey="test.left" defaultWidth={200}>
        <div data-testid="child" />
      </ResizablePanel>,
    )

    await userEvent.type(screen.getByRole('separator'), '{ArrowRight}')

    expect(widthOf()).toBe(216)
  })

  it('refuses to shrink past its minimum', async () => {
    render(
      <ResizablePanel storageKey="test.min" defaultWidth={200} minWidth={200} side="right">
        <div data-testid="child" />
      </ResizablePanel>,
    )

    await userEvent.type(screen.getByRole('separator'), '{ArrowRight}')

    expect(widthOf()).toBe(200)
  })
})
