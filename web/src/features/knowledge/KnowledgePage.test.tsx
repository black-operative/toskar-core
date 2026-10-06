import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api } from '@/lib/api'
import type { AIProfile, KnowledgeSource } from '@/types/api'
import { KnowledgePage } from './KnowledgePage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listKnowledge: vi.fn(),
      getProfiles: vi.fn(),
      getSettings: vi.fn(),
      getModels: vi.fn(),
      updateProfile: vi.fn(),
      createKnowledge: vi.fn(),
    },
  }
})

const general: AIProfile = {
  id: 'general-assistant',
  name: 'General',
  purpose: 'general',
  orchestrator_id: 'simple',
  roles: [],
  node_policy: { mode: 'automatic' },
  knowledge_sources: ['ks-used'],
}
const research: AIProfile = { ...general, id: 'research', name: 'Research', knowledge_sources: [] }

const source = (id: string, name: string): KnowledgeSource =>
  ({ id, name, kind: 'path', path: `~/${name}`, status: 'ready', chunk_count: 3 }) as KnowledgeSource

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <KnowledgePage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('KnowledgePage', () => {
  beforeEach(() => {
    vi.mocked(api.listKnowledge).mockResolvedValue([source('ks-used', 'Price list'), source('ks-new', 'Returns policy')])
    vi.mocked(api.getProfiles).mockResolvedValue([general, research])
    vi.mocked(api.getSettings).mockResolvedValue({} as never)
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.updateProfile).mockImplementation(async (_id, p) => p as AIProfile)
    vi.mocked(api.createKnowledge).mockResolvedValue(source('ks-made', 'Notes'))
  })

  it('says which profiles use a source, and offers to use one nothing uses', async () => {
    renderPage()
    expect(await screen.findByText('Used in chats with General')).toBeInTheDocument()
    expect(screen.getByText(/No profile uses this yet/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Use with General' }))
    await waitFor(() =>
      expect(api.updateProfile).toHaveBeenCalledWith('general-assistant', expect.objectContaining({ knowledge_sources: ['ks-used', 'ks-new'] })),
    )
  })

  it('adds a new source to the chosen profiles when it connects', async () => {
    renderPage()
    const general = await screen.findByRole('checkbox', { name: 'General' })
    expect(general).toBeChecked()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Research' }))
    fireEvent.change(screen.getByPlaceholderText('~/Documents/inventory.csv'), { target: { value: '~/Notes' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    await waitFor(() => expect(api.updateProfile).toHaveBeenCalledTimes(2))
    expect(api.updateProfile).toHaveBeenCalledWith('general-assistant', expect.objectContaining({ knowledge_sources: ['ks-used', 'ks-made'] }))
    expect(api.updateProfile).toHaveBeenCalledWith('research', expect.objectContaining({ knowledge_sources: ['ks-made'] }))
  })

  it('explains knowledge and offers examples when nothing is connected', async () => {
    vi.mocked(api.listKnowledge).mockResolvedValue([])
    renderPage()
    expect(await screen.findByRole('heading', { name: 'How knowledge works' })).toBeInTheDocument()
    expect(screen.getByText(/such as General/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /A database/ }))
    expect(screen.getByRole('button', { name: 'Database' })).toHaveAttribute('aria-pressed', 'true')
  })
})
