import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api } from '@/lib/api'
import type { ToolRecord } from '@/types/api'
import { ToolsPage } from './ToolsPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listTools: vi.fn(),
      listToolRuns: vi.fn(),
      listConnectors: vi.fn(),
      listMCPServers: vi.fn(),
      getMediaSetup: vi.fn(),
    },
  }
})

const search: ToolRecord = {
  id: 'internet.search',
  name: 'Web Search',
  description: 'Search the public internet.',
  capability: 'internet',
  source: 'builtin',
  schema: '{}',
  default_policy: 'allow',
  risk: 'read',
  enabled: true,
  profiles: [],
}

function renderPage(at = '/tools') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[at]}>
        <ToolsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ToolsPage', () => {
  beforeEach(() => {
    vi.mocked(api.listTools).mockResolvedValue([search])
    vi.mocked(api.listToolRuns).mockResolvedValue([])
    vi.mocked(api.listConnectors).mockResolvedValue([])
    vi.mocked(api.listMCPServers).mockResolvedValue([])
    vi.mocked(api.getMediaSetup).mockResolvedValue(null as never)
  })

  it('opens on your tools, and moves between the tabs', async () => {
    renderPage()
    expect(screen.getByRole('tab', { name: 'Your tools' })).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByText('Web Search')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: 'Add more' }))
    expect(screen.getByRole('tab', { name: 'Add more' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByText('Web Search')).not.toBeInTheDocument()
  })

  it('opens Add more from a link', () => {
    renderPage('/tools?tab=add')
    expect(screen.getByRole('tab', { name: 'Add more' })).toHaveAttribute('aria-selected', 'true')
  })

  it('explains tools and links to profiles', () => {
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: 'How it works' }))
    expect(screen.getByRole('heading', { name: 'How tools work' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Profiles & Orchestration' })).toHaveAttribute('href', '/profiles')
  })
})
