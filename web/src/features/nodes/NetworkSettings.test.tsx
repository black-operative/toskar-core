import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import { NetworkSettings } from './NetworkSettings'

vi.mock('@/lib/api', () => ({
  api: {
    getSettings: vi.fn(),
    updateSettings: vi.fn(),
  },
}))
vi.mock('./ExternalServer', () => ({ ExternalServer: () => <p>External server form</p> }))

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <NetworkSettings />
    </QueryClientProvider>,
  )
}

describe('NetworkSettings', () => {
  beforeEach(() => {
    vi.mocked(api.getSettings).mockResolvedValue({ discovery_enabled: true } as never)
    vi.mocked(api.updateSettings).mockResolvedValue({ discovery_enabled: false } as never)
    useUIStore.setState({ advancedMode: false })
  })

  it('turns finding other computers off from the Computers page', async () => {
    renderIt()
    expect(screen.getByRole('heading', { name: 'Network & access' })).toBeInTheDocument()
    const toggle = await screen.findByRole('switch', { name: 'Find other computers' })
    await waitFor(() => expect(toggle).toBeEnabled())
    fireEvent.click(toggle)
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ discovery_enabled: false }))
  })

  it('shows the external server only in advanced mode', async () => {
    const { unmount } = renderIt()
    await screen.findByRole('switch', { name: 'Find other computers' })
    expect(screen.queryByText('External server form')).not.toBeInTheDocument()
    unmount()
    useUIStore.setState({ advancedMode: true })
    renderIt()
    expect(await screen.findByText('External server form')).toBeInTheDocument()
  })
})
