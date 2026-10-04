import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { MCPShare } from '@/types/api'
import { ShareWithApps } from './ShareWithApps'

vi.mock('@/lib/api', () => ({ api: { mcpShare: vi.fn() } }))

function renderIt() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ShareWithApps />
    </QueryClientProvider>,
  )
}

describe('ShareWithApps', () => {
  it('gives each app its settings, with the address and key when needed', async () => {
    vi.mocked(api.mcpShare).mockResolvedValue({ url: 'http://127.0.0.1:7444/mcp', command: '/opt/homebrew/bin/toskarctl', args: ['mcp'], needs_key: true })
    renderIt()
    const settings = await screen.findByLabelText('json settings')
    expect(settings.textContent).toContain('"command": "/opt/homebrew/bin/toskarctl"')
    expect(settings.textContent).toContain('"TOSKAR_URL": "http://127.0.0.1:7444"')
    expect(settings.textContent).toContain('"TOSKAR_API_KEY": "<your API key>"')
    fireEvent.click(screen.getByRole('tab', { name: 'Claude Code' }))
    expect(screen.getByLabelText('bash settings').textContent).toBe(
      'claude mcp add --transport http toskar http://127.0.0.1:7444/mcp --header "Authorization: Bearer <your API key>"',
    )
    fireEvent.click(screen.getByRole('tab', { name: 'VS Code' }))
    expect(screen.getByLabelText('json settings').textContent).toContain('"type": "http"')
  })

  it('gives yggctl, from an app built before the rename, the settings names it reads', async () => {
    vi.mocked(api.mcpShare).mockResolvedValue({ url: 'http://127.0.0.1:7444/mcp', command: 'C:\\Apps\\yggctl.exe', args: ['mcp'], needs_key: true })
    renderIt()
    const settings = await screen.findByLabelText('json settings')
    expect(settings.textContent).toContain('"YGGDRASIL_URL": "http://127.0.0.1:7444"')
    expect(settings.textContent).toContain('"YGGDRASIL_API_KEY": "<your API key>"')
  })

  it('leaves out the address and key on the default local setup', async () => {
    vi.mocked(api.mcpShare).mockResolvedValue({ url: 'http://127.0.0.1:7331/mcp', command: 'yggctl', args: ['mcp'], needs_key: false })
    renderIt()
    const settings = await screen.findByLabelText('json settings')
    expect(settings.textContent).not.toContain('env')
  })

  // The release screenshots once served [] here, which blanked the page.
  it('hides itself instead of crashing on a reply that is not a share', async () => {
    vi.mocked(api.mcpShare).mockResolvedValue([] as unknown as MCPShare)
    const { container } = renderIt()
    await waitFor(() => expect(api.mcpShare).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
  })
})
