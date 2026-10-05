import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { Connector } from '@/types/api'
import { ConnectedServices } from './ConnectedServices'

vi.mock('@/lib/api', () => ({
  api: {
    listConnectors: vi.fn(),
    connectService: vi.fn(),
    checkConnector: vi.fn(),
    disconnectService: vi.fn(),
  },
}))

const github: Connector = {
  id: 'github',
  name: 'GitHub',
  description: 'Search and read issues and pull requests, and comment on them.',
  scopes: 'Create a fine-grained personal access token limited to the repositories you want.',
  fields: [
    { key: 'token', label: 'Personal access token', secret: true },
    { key: 'api_url', label: 'API address', secret: false, optional: true },
  ],
  connected: false,
  tools: [
    { id: 'github.search', name: 'Search GitHub', description: '', risk: 'read', default_policy: 'allow' },
    { id: 'github.comment', name: 'Comment on GitHub', description: '', risk: 'write', default_policy: 'ask' },
  ],
}

function renderIt() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ConnectedServices />
    </QueryClientProvider>,
  )
}

describe('ConnectedServices', () => {
  beforeEach(() => {
    let current = github
    vi.mocked(api.listConnectors).mockReset().mockImplementation(async () => [current])
    vi.mocked(api.connectService).mockReset().mockImplementation(async (_id, values) => {
      if (values.token !== 'github_pat_good') throw new Error('could not connect to GitHub: the token was not accepted (401)')
      current = { ...github, connected: true, status: 'connected', account: '@octo', values: { token: '••••good' } }
      return current
    })
    vi.mocked(api.disconnectService).mockReset().mockImplementation(async () => {
      current = github
      return null
    })
  })

  it('connects with a token, shows the account, and never shows the token again', async () => {
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }))
    expect(screen.getByText(/fine-grained personal access token/)).toBeInTheDocument()
    const token = screen.getByLabelText('Personal access token')
    expect(token).toHaveAttribute('type', 'password')

    fireEvent.change(token, { target: { value: 'wrong' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await screen.findByText(/the token was not accepted/)).toBeInTheDocument()

    fireEvent.change(token, { target: { value: 'github_pat_good' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await screen.findByText('Connected as @octo')).toBeInTheDocument()
    expect(screen.getByText(/Comment on GitHub \(asks first\)/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Change' }))
    const again = screen.getByLabelText('Personal access token')
    expect(again).toHaveValue('')
    expect(again).toHaveAttribute('placeholder', 'Stored (••••good). Leave blank to keep it.')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }))
    await waitFor(() => expect(screen.getByText('Not connected')).toBeInTheDocument())
  })
})
