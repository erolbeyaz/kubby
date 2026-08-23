import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { YamlFallback } from './YamlFallback'

const YAML = `# a comment
apiVersion: v1
kind: Pod
metadata:
  name: payments-api
  replicas: 3
`

describe('YamlFallback', () => {
  // Swapping unstyled text for a highlighted editor reads as the view breaking and
  // repairing itself, so what is shown while the editor loads is coloured the same way.
  it('colours keys apart from their values', () => {
    render(<YamlFallback value={YAML} />)

    expect(screen.getByText('kind')).toHaveStyle({ color: '#7dd3fc' })
    expect(screen.getByText('Pod')).toHaveStyle({ color: '#e8e8e8' })
    expect(screen.getByText('# a comment')).toHaveStyle({ color: '#6b6b6b' })

    // "3" is both a line number and a value here, so the value is found by its key.
    const replicas = screen.getByText('replicas').closest('span')?.parentElement
    expect(replicas).toHaveTextContent('replicas: 3')
    expect(within(replicas as HTMLElement).getByText('3')).toHaveStyle({ color: '#f0a868' })
  })

  it('numbers every line', () => {
    render(<YamlFallback value={YAML} />)

    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText('6')).toBeInTheDocument()
  })
})
