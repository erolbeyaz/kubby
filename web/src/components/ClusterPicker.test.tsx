import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/lib/api'

import { ClusterPicker } from './ClusterPicker'

const base = {
  environmentLabel: '', color: '', authSource: 'kubeconfig', insecureSkipTlsVerify: false,
  metricsAvailable: true, readOnly: false, impersonationEnabled: false, qpsLimit: 50,
  metricsInsecureSkipVerify: false,
  logsInsecureSkipVerify: false,
}

const CLUSTERS = [
  {
    ...base, id: 'c1', name: 'kubby-mini', environment: 'test', displayEnvironment: 'test',
    apiServerUrl: 'https://x:8443', credentialStatus: 'valid', k8sVersion: 'v1.34.4',
  },
  {
    ...base, id: 'c2', name: 'prod-app', environment: 'prod', displayEnvironment: 'Production',
    apiServerUrl: 'https://y:6443', credentialStatus: 'unreachable',
    statusDetail: 'dial tcp: no such host',
  },
] as unknown as Cluster[]

function renderPicker() {
  const onSelect = vi.fn()
  render(
    <ClusterPicker
      clusters={CLUSTERS}
      current={CLUSTERS[0] as Cluster}
      canManage
      onSelect={onSelect}
      onManage={vi.fn()}
    />,
  )
  return { onSelect }
}

describe('ClusterPicker', () => {
  // The last door into the trap Home was built to prevent: switching to a cluster nobody
  // can reach lands the reader on a connection error, with every screen under it showing
  // the same thing.
  it('will not switch to a cluster it cannot reach', async () => {
    const { onSelect } = renderPicker()

    await userEvent.click(screen.getByRole('button', { name: 'Select cluster' }))

    const broken = screen.getByRole('option', { name: /prod-app/ })
    expect(broken).toBeDisabled()

    await userEvent.click(broken)
    expect(onSelect).not.toHaveBeenCalled()
  })

  // Disabled without a reason is a dead end. The row says why, in the space its tier and
  // version would otherwise take.
  it('says why the row cannot be chosen', async () => {
    renderPicker()

    await userEvent.click(screen.getByRole('button', { name: 'Select cluster' }))
    expect(screen.getByRole('option', { name: /not answering/ })).toBeInTheDocument()
    expect(screen.getByTitle('dial tcp: no such host')).toBeInTheDocument()
  })

  it('still switches to one that answers', async () => {
    const { onSelect } = renderPicker()

    await userEvent.click(screen.getByRole('button', { name: 'Select cluster' }))
    await userEvent.click(screen.getByRole('option', { name: /kubby-mini/ }))

    expect(onSelect).toHaveBeenCalledWith('c1')
  })
})
