import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { NavigationRail } from './NavigationRail'

function show(current: { id: string; name: string } | null) {
  const types = vi.spyOn(api, 'resourceTypes').mockResolvedValue({ types: [] })

  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <NavigationRail
        clusters={[]}
        current={current as never}
        canManage
        selectedType={null}
        onSelectCluster={() => {}}
        onManageClusters={() => {}}
        onSelectType={() => {}}
      />
    </QueryClientProvider>,
  )
  return types
}

afterEach(() => vi.restoreAllMocks())

describe('NavigationRail', () => {
  // A fresh install had no rail, because the rail lived inside a screen that needs a
  // cluster — so the way to add the first one was not on screen at all.
  it('is there before any cluster exists', async () => {
    show(null)

    expect(await screen.findByRole('button', { name: /Home/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Kubby Settings/ })).toBeInTheDocument()
  })

  // Asking a cluster that is not there for its kinds is a request that can only fail.
  it('asks for no kinds when there is no cluster to ask', () => {
    const types = show(null)
    expect(types).not.toHaveBeenCalled()
  })

  it('lists a cluster\'s kinds once there is one', async () => {
    const types = show({ id: 'c1', name: 'prod' })
    await screen.findByRole('button', { name: /Home/ })
    expect(types).toHaveBeenCalledWith('c1', expect.anything())
  })
})
