import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import type { DevicePairing, SettingsView } from '@/types/api'
import { ConnectPhone } from './ConnectPhone'

vi.mock('@/lib/api', () => ({
  ApiError: class extends Error {},
  api: { getSettings: vi.fn(), startDevicePairing: vi.fn(), getDevicePairing: vi.fn(), cancelDevicePairing: vi.fn(async () => null) },
}))

const expires = new Date(Date.now() + 10 * 60_000).toISOString()
const shown: DevicePairing = { code: '123456', expires_at: expires, state: 'waiting', address: '192.168.1.10:7331', reachable: true }

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>
        <ConnectPhone onClose={() => {}} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ConnectPhone', () => {
  let status: DevicePairing
  beforeEach(async () => {
    await applyLanguage('en')
    status = { ...shown, code: undefined }
    vi.mocked(api.getSettings).mockResolvedValue({ lan_api_enabled: false, discovery_enabled: true, node_name: 'Studio' } as SettingsView)
    vi.mocked(api.startDevicePairing).mockResolvedValue(shown)
    vi.mocked(api.getDevicePairing).mockImplementation(async () => status)
  })

  it('turns on network access, shows the code, and says when the phone connects', async () => {
    renderIt()
    expect(await screen.findByText(/will let devices on your network connect/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Allow and show a code' }))
    await waitFor(() => expect(api.startDevicePairing).toHaveBeenCalledWith(true))
    expect(await screen.findByText('123 456')).toBeInTheDocument()
    expect(screen.getByText('Choose Studio under On this network.')).toBeInTheDocument()
    expect(screen.getByText('192.168.1.10:7331')).toBeInTheDocument()

    status = { ...status, state: 'connected', device: { id: 'k1', name: "Sam's iPhone", prefix: 'ygg_', created_at: '', revoked: false, kind: 'device' } }
    expect(await screen.findByText("Sam's iPhone is connected.", {}, { timeout: 4000 })).toBeInTheDocument()
    expect(screen.queryByText('123 456')).toBeNull()
  })

  it('offers a new code when the code expires', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ lan_api_enabled: true, node_name: 'Studio' } as SettingsView)
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Show a code' }))
    await waitFor(() => expect(api.startDevicePairing).toHaveBeenCalledWith(false))
    await screen.findByText('123 456')
    // Not announced on the network, so the phone needs the address.
    expect(screen.getByText("Enter this computer's address:")).toBeInTheDocument()
    expect(screen.queryByText(/under On this network/)).toBeNull()
    status = { ...status, state: 'expired' }
    expect(await screen.findByText('The code expired.', {}, { timeout: 4000 })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show a new code' })).toBeInTheDocument()
  })
})
