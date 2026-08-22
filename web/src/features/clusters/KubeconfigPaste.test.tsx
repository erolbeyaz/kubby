import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { KubeconfigPaste } from './KubeconfigPaste'

const VALID_RESULT = {
  contexts: [
    {
      name: 'kubby-test',
      clusterName: 'kubby-test',
      userName: 'kubby-test-user',
      server: 'https://127.0.0.1:6550',
      authMethod: 'token',
      insecureSkipTlsVerify: false,
      hasCertificateAuthority: true,
      blocked: false,
    },
  ],
  currentContext: 'kubby-test',
  probe: {
    status: 'valid',
    k8sVersion: 'v1.35.5+k3s1',
    nodeCount: 3,
    metricsAvailable: true,
    permissions: ['list pods', 'read pod logs'],
  },
}

function mockValidate(response: { status: number; body: unknown }) {
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(input instanceof Request ? input.url : input.toString())
      void init
      return Promise.resolve(
        new Response(JSON.stringify(response.body), {
          status: response.status,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }),
  )
  return calls
}

function renderPaste(onConfirm = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <KubeconfigPaste confirmLabel="Add cluster" onConfirm={onConfirm} />
    </QueryClientProvider>,
  )
  return onConfirm
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('KubeconfigPaste', () => {
  // Nothing may be saved until the user has seen what the credential is (ADR-018).
  it('will not offer to save before the connection has been checked', async () => {
    mockValidate({ status: 200, body: VALID_RESULT })
    renderPaste()

    expect(screen.queryByRole('button', { name: 'Add cluster' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check connection' })).toBeDisabled()

    await userEvent.type(screen.getByLabelText('Kubeconfig'), 'apiVersion: v1')
    expect(screen.getByRole('button', { name: 'Check connection' })).toBeEnabled()
  })

  it('shows what the credential is and can do before saving', async () => {
    mockValidate({ status: 200, body: VALID_RESULT })
    const onConfirm = renderPaste()

    await userEvent.type(screen.getByLabelText('Kubeconfig'), 'apiVersion: v1')
    await userEvent.click(screen.getByRole('button', { name: 'Check connection' }))

    expect(await screen.findByText('Connected')).toBeInTheDocument()
    expect(screen.getByText('v1.35.5+k3s1')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('list pods')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Add cluster' }))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('explains a refused kubeconfig instead of failing silently', async () => {
    mockValidate({
      status: 422,
      body: {
        error: 'exec-plugin based authentication is not supported: user "eks-user" uses an exec plugin',
        requestId: 'req-1',
      },
    })
    renderPaste()

    await userEvent.type(screen.getByLabelText('Kubeconfig'), 'apiVersion: v1')
    await userEvent.click(screen.getByRole('button', { name: 'Check connection' }))

    expect(await screen.findByText(/exec-plugin based authentication/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add cluster' })).not.toBeInTheDocument()
  })

  it('warns when TLS verification is disabled', async () => {
    mockValidate({
      status: 200,
      body: {
        ...VALID_RESULT,
        contexts: [{ ...VALID_RESULT.contexts[0], insecureSkipTlsVerify: true, hasCertificateAuthority: false }],
      },
    })
    renderPaste()

    await userEvent.type(screen.getByLabelText('Kubeconfig'), 'apiVersion: v1')
    await userEvent.click(screen.getByRole('button', { name: 'Check connection' }))

    expect(await screen.findByText(/weaker transport security/i)).toBeInTheDocument()
    expect(screen.getByText(/insecure-skip-tls-verify/)).toBeInTheDocument()
  })

  it('lets the user pick a context and refuses to save a blocked one', async () => {
    mockValidate({
      status: 200,
      body: {
        contexts: [
          VALID_RESULT.contexts[0],
          {
            name: 'evil',
            clusterName: 'meta',
            userName: 'u',
            server: 'https://169.254.169.254',
            authMethod: 'token',
            insecureSkipTlsVerify: true,
            hasCertificateAuthority: false,
            blocked: true,
            problem: 'cluster address is not allowed: 169.254.169.254 is link-local',
          },
        ],
        currentContext: 'kubby-test',
        probe: VALID_RESULT.probe,
      },
    })
    renderPaste()

    await userEvent.type(screen.getByLabelText('Kubeconfig'), 'apiVersion: v1')
    await userEvent.click(screen.getByRole('button', { name: 'Check connection' }))

    await waitFor(() => expect(screen.getByText(/2 contexts/)).toBeInTheDocument())

    // The blocked context is visible — so the user knows why — but cannot be chosen.
    expect(screen.getByText(/link-local/)).toBeInTheDocument()
    const radios = screen.getAllByRole('radio')
    expect(radios[1]).toBeDisabled()
  })

  it('reports a rejected credential distinctly from an unreachable cluster', async () => {
    mockValidate({
      status: 200,
      body: {
        ...VALID_RESULT,
        probe: {
          status: 'invalid',
          detail: 'the credential was rejected; the token may have expired or been revoked',
          metricsAvailable: false,
        },
      },
    })
    renderPaste()

    await userEvent.type(screen.getByLabelText('Kubeconfig'), 'apiVersion: v1')
    await userEvent.click(screen.getByRole('button', { name: 'Check connection' }))

    expect(await screen.findByText('The credential was rejected')).toBeInTheDocument()
    expect(screen.getByText(/token may have expired/)).toBeInTheDocument()
  })
})
