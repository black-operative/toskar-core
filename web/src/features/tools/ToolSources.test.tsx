import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { MCPGalleryEntry, MCPServer } from '@/types/api'
import { ToolSources } from './ToolSources'

vi.mock('@/lib/api', () => ({
  api: {
    listMCPServers: vi.fn(),
    addMCPServer: vi.fn(),
    updateMCPServer: vi.fn(),
    checkMCPServer: vi.fn(),
    removeMCPServer: vi.fn(),
    signInMCPServer: vi.fn(),
    signOutMCPServer: vi.fn(),
    setToolEnabled: vi.fn(),
    mcpGallery: vi.fn(),
    mcpImportCandidates: vi.fn(),
    parseMCP: vi.fn(),
    mcpServerLogs: vi.fn(),
    mcpServerPrompts: vi.fn(),
    getMCPPrompt: vi.fn(),
  },
}))

const folders: MCPGalleryEntry = {
  id: 'folders',
  name: 'Folders',
  description: 'Let the AI read, search, and organize files in folders you choose.',
  category: 'Files & data',
  remote: false,
  fields: [{ key: 'folders', label: 'Folders to share', kind: 'folders', default: '~/Documents' }],
}

const linearGallery: MCPGalleryEntry = {
  id: 'linear',
  name: 'Linear',
  description: 'Find, create, and update Linear issues.',
  category: 'Work apps',
  remote: true,
  sign_in: true,
  fields: [],
}

function server(over: Partial<MCPServer>): MCPServer {
  return {
    id: 'folders',
    name: 'Folders',
    where: 'local',
    env: [],
    headers: [],
    enabled: true,
    allow_sampling: false,
    always_offer: false,
    keywords: [],
    status: 'ready',
    running: false,
    has_resources: false,
    has_prompts: false,
    tools: [
      { id: 'folders.read_file', name: 'Read file', remote_name: 'read_file', description: '', risk: 'read', policy: 'allow', changed: false, enabled: true },
      { id: 'folders.write_file', name: 'Write file', remote_name: 'write_file', description: '', risk: 'write', policy: 'ask', changed: false, enabled: true },
    ],
    added_at: '2026-10-01T00:00:00Z',
    ...over,
  }
}

function renderIt() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ToolSources />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// A failed list is an error with Try again, never "No tool sources yet".
describe('ToolSources when the list fails to load', () => {
  it('says it could not load, offers Try again, and loads on retry', async () => {
    vi.mocked(api.listMCPServers).mockRejectedValueOnce(new Error('database is locked')).mockResolvedValue([])
    renderIt()
    expect(await screen.findByRole('alert')).toHaveTextContent("Yggdrasil couldn't load this")
    expect(screen.queryByText(/No tool sources yet/)).toBeNull()
    expect(screen.getByText('database is locked')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText(/No tool sources yet/)).toBeInTheDocument()
  })
})

describe('ToolSources', () => {
  let current: MCPServer[]
  beforeEach(() => {
    current = []
    vi.mocked(api.listMCPServers).mockReset().mockImplementation(async () => current)
    vi.mocked(api.mcpGallery).mockReset().mockResolvedValue([folders, linearGallery])
    vi.mocked(api.mcpImportCandidates).mockReset().mockResolvedValue([])
    vi.mocked(api.addMCPServer).mockReset()
    vi.mocked(api.parseMCP).mockReset()
    vi.mocked(api.updateMCPServer).mockReset()
    vi.mocked(api.signInMCPServer).mockReset()
  })

  it('adds a gallery entry with one answer', async () => {
    vi.mocked(api.addMCPServer).mockImplementation(async () => {
      current = [server({})]
      return { server: current[0] }
    })
    renderIt()
    fireEvent.click(await screen.findByText(/No tool sources yet/))
    fireEvent.click(await screen.findByText('Folders'))
    expect(screen.getByLabelText('Folders to share')).toHaveValue('~/Documents')
    fireEvent.change(screen.getByLabelText('Folders to share'), { target: { value: '~/Projects' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add Folders' }))
    await screen.findByText(/is ready with 2 tools/)
    expect(screen.getByText(/1 that read, and 1 that change things and ask you first/)).toBeInTheDocument()
    expect(api.addMCPServer).toHaveBeenCalledWith({ preset: 'folders', values: { folders: '~/Projects' } })
    expect(await screen.findByText(/Ready · 2 tools · starts when needed/)).toBeInTheDocument()
  })

  it('opens the sign-in for a service that needs one', async () => {
    const popup = { closed: false, close: vi.fn(), location: { href: '' } }
    const open = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    vi.mocked(api.addMCPServer).mockImplementation(async () => {
      current = [server({ id: 'linear', name: 'Linear', where: 'remote', status: 'sign_in', tools: [] })]
      return { server: current[0], sign_in_url: 'https://linear.example/authorize?x=1' }
    })
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Add tools' }))
    fireEvent.click(await screen.findByText('Linear'))
    fireEvent.click(screen.getByRole('button', { name: 'Sign in and add Linear' }))
    await screen.findByText(/needs you to sign in/)
    // The window opened during the click is sent to the sign-in.
    expect(open).toHaveBeenCalledWith('about:blank', 'toskar-sign-in', expect.any(String))
    expect(popup.location.href).toBe('https://linear.example/authorize?x=1')
    expect(await screen.findByRole('button', { name: 'Sign in to Linear' })).toBeInTheDocument()
    open.mockRestore()
  })

  it('reads pasted settings and asks for the placeholder', async () => {
    vi.mocked(api.parseMCP).mockResolvedValue([
      {
        spec: { name: 'Brave Search', command: 'npx', args: ['-y', '@brave/brave-search-mcp-server'], env: { BRAVE_API_KEY: 'YOUR_KEY_HERE' } },
        needs: [{ key: 'env:BRAVE_API_KEY', label: 'BRAVE_API_KEY', secret: true }],
      },
    ])
    vi.mocked(api.addMCPServer).mockImplementation(async () => {
      current = [server({ id: 'brave-search', name: 'Brave Search' })]
      return { server: current[0] }
    })
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Add tools' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Paste' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '{"mcpServers": {}}' } })
    fireEvent.click(screen.getByRole('button', { name: 'Read it' }))
    const add = await screen.findByRole('button', { name: 'Add Brave Search' })
    expect(add).toBeDisabled()
    fireEvent.change(screen.getByLabelText('BRAVE_API_KEY'), { target: { value: 'BSA123' } })
    fireEvent.click(add)
    await screen.findByText(/is ready with/)
    expect(api.addMCPServer).toHaveBeenCalledWith({
      spec: expect.objectContaining({ name: 'Brave Search', command: 'npx' }),
      values: { 'env:BRAVE_API_KEY': 'BSA123' },
    })
  })

  it('shows what is wrong and how to fix it', async () => {
    current = [
      server({ status: 'error', error: 'the server stopped (exit code 1): EACCES' }),
      server({ id: 'notion', name: 'Notion', where: 'remote', status: 'sign_in', tools: [] }),
      server({ id: 'time', name: 'Time zones', missing: 'uv' }),
    ]
    renderIt()
    expect(await screen.findByText(/Needs attention: the server stopped/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sign in to Notion' })).toBeInTheDocument()
    expect(screen.getByText('Needs uv on this computer')).toBeInTheDocument()
  })

  it('changes a tool to ask first and turns a source off', async () => {
    current = [server({})]
    vi.mocked(api.updateMCPServer).mockImplementation(async (_id, u) => {
      current = [server({ enabled: u.enabled ?? true, status: u.enabled === false ? 'off' : 'ready' })]
      return current[0]
    })
    renderIt()
    fireEvent.click(await screen.findByText('Folders'))
    const askFirst = screen.getAllByLabelText('Ask first')
    expect(askFirst[0]).not.toBeChecked()
    expect(askFirst[1]).toBeChecked()
    fireEvent.click(askFirst[0])
    await waitFor(() => expect(api.updateMCPServer).toHaveBeenCalledWith('folders', { policies: { read_file: 'ask' } }))
    fireEvent.click(screen.getByRole('switch', { name: 'Turn off Folders' }))
    await waitFor(() => expect(api.updateMCPServer).toHaveBeenCalledWith('folders', { enabled: false }))
    expect(await screen.findByText('Off')).toBeInTheDocument()
  })
})
