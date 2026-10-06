import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type { AIProfile } from '@/types/api'
import { ProfilesPage } from './ProfilesPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProfiles: vi.fn(),
      getModels: vi.fn(),
      getNodes: vi.fn(),
      listKnowledge: vi.fn(),
      updateProfile: vi.fn(),
    },
  }
})

const general: AIProfile = {
  id: 'general-assistant',
  name: 'General',
  purpose: 'general',
  orchestrator_id: 'simple',
  roles: [{ role: 'assistant', model_id: '', required: true }],
  node_policy: { mode: 'automatic' },
  tools: [],
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ProfilesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ProfilesPage editor', () => {
  beforeEach(() => {
    useUIStore.setState({ advancedMode: true })
    vi.mocked(api.getProfiles).mockResolvedValue([general])
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.getNodes).mockResolvedValue([])
    vi.mocked(api.listKnowledge).mockResolvedValue([])
    vi.mocked(api.updateProfile).mockImplementation(async (_id, p) => p as AIProfile)
  })

  it('splits the editor into tabs and keeps unused roles behind Show all roles', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    expect(screen.getByRole('tab', { name: 'Models' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Primary')
    expect(screen.queryByText('Reviewer')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Show all roles/ }))
    expect(screen.getByText('Reviewer')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: 'Tools' }))
    expect(screen.getByRole('switch', { name: 'Internet' })).toHaveAttribute('aria-checked', 'false')
  })

  it('saves a profile with no tools without turning any on', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save profile' }))
    await waitFor(() => expect(api.updateProfile).toHaveBeenCalled())
    const saved = vi.mocked(api.updateProfile).mock.calls[0][1]
    expect((saved.tools ?? []).filter((t) => t.policy !== 'deny')).toEqual([])
  })
})
