import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api } from '@/lib/api'
import type { Node } from '@/types/api'
import { NodesPage } from './NodesPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getNodes: vi.fn(),
      listPairingOffers: vi.fn(),
      listRunningModels: vi.fn(),
      getModels: vi.fn(),
      revokeNode: vi.fn(),
      getSettings: vi.fn(),
    },
  }
})

const local = { id: 'local', name: 'This Mac', is_local: true, paired: true, status: 'online' } as Node
const studio = { id: 'studio', name: 'Studio', is_local: false, paired: true, status: 'online' } as Node

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <NodesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('NodesPage', () => {
  beforeEach(() => {
    vi.mocked(api.getNodes).mockResolvedValue([local, studio])
    vi.mocked(api.listPairingOffers).mockResolvedValue([])
    vi.mocked(api.listRunningModels).mockResolvedValue([])
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.getSettings).mockResolvedValue({} as never)
    vi.mocked(api.revokeNode).mockResolvedValue(null as never)
  })
  afterEach(() => vi.restoreAllMocks())

  it('is titled like the sidebar', async () => {
    renderPage()
    expect(screen.getByRole('heading', { level: 1, name: 'Computers' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Studio' })).toBeInTheDocument()
  })

  it('explains how to add a computer while this is the only one, with the real button names', async () => {
    vi.mocked(api.getNodes).mockResolvedValue([local])
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Add another computer' })).toBeInTheDocument()
    expect(screen.getByText(/choose Add to team, then approve it on the other computer/)).toBeInTheDocument()
    expect(screen.getByText(/use Add by command/)).toBeInTheDocument()
  })

  it('asks before removing a computer from the team', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    renderPage()
    const more = await screen.findByRole('button', { name: 'More actions for Studio' })
    fireEvent.click(more)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Remove from team' }))
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(api.revokeNode).not.toHaveBeenCalled()
    fireEvent.click(more)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Remove from team' }))
    await waitFor(() => expect(api.revokeNode).toHaveBeenCalledWith('studio'))
  })
})
