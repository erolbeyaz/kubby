import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ResourceTree } from './ResourceTree'

const TYPES = [
  { key: 'pods', kind: 'Pod', category: 'workload', namespaced: true, cached: true, verbs: ['list'] },
  { key: 'deployments', kind: 'Deployment', category: 'workload', namespaced: true, cached: false, verbs: ['list'] },
  { key: 'services', kind: 'Service', category: 'network', namespaced: true, cached: false, verbs: ['list'] },
]

function renderTree(selected: string | null = null) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ types: TYPES }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    ),
  )

  const onSelectType = vi.fn()
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <ResourceTree
        clusterId="c1"
        selectedType={selected}
        canManageSettings
        onSelectType={onSelectType}
      />
    </QueryClientProvider>,
  )
  return { onSelectType }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ResourceTree', () => {
  // The Overview is one chip beside the mark now. A rail entry asking the same question
  // is a second answer to it, and the Workloads section had a third.
  it('carries no Overview of its own', async () => {
    renderTree()

    await screen.findByRole('button', { name: 'Nodes' })
    expect(screen.queryByRole('button', { name: 'Overview' })).not.toBeInTheDocument()
    // Home and Nodes, and nothing else, above the rule.
    expect(screen.getByRole('button', { name: 'Home' })).toBeInTheDocument()
  })

  // A Helm release is a bundle of the things listed under Workloads, not a peer of the
  // node list, so it sits inside that section rather than above it.
  it('files Applications under Workloads and leaves only Nodes above', async () => {
    const { onSelectType } = renderTree()

    const applications = await screen.findByRole('button', { name: 'Applications' })
    await userEvent.click(applications)
    expect(onSelectType).toHaveBeenCalledWith('applications')

    // Indented with the kinds it belongs among, rather than flush with the top entries.
    expect(applications.className).toContain('pl-6')
    expect(screen.getByRole('button', { name: 'Nodes' }).className).toContain('px-2')
  })

  // The way out. A cluster that stops answering leaves every screen under it showing the
  // connection error; without a fixed exit in the rail the reader is stuck on it.
  it('heads the rail with Home', async () => {
    const { onSelectType } = renderTree()

    await userEvent.click(await screen.findByRole('button', { name: 'Home' }))
    expect(onSelectType).toHaveBeenCalledWith('home')
  })

  it('still opens a kind', async () => {
    const { onSelectType } = renderTree()

    await userEvent.click(await screen.findByRole('button', { name: 'Pods' }))
    await waitFor(() => expect(onSelectType).toHaveBeenCalledWith('pods'))
  })
})
