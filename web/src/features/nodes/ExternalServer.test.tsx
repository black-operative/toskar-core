import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { ExternalServer } from './ExternalServer'

vi.mock('@/lib/api', () => ({ api: { getExternalServer: vi.fn(), setExternalServer: vi.fn() } }))

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ExternalServer />
    </QueryClientProvider>,
  )
}

describe('ExternalServer', () => {
  beforeEach(() => {
    vi.mocked(api.getExternalServer).mockResolvedValue({ base_url: '', has_key: false, models: [] })
    vi.mocked(api.setExternalServer).mockResolvedValue({ base_url: 'https://api.example', has_key: true, models: ['gpt-4o-mini', 'llama-70b'] })
  })

  it('saves the URL and key, and lists the models it offers', async () => {
    renderIt()
    fireEvent.change(await screen.findByLabelText('Base URL'), { target: { value: 'https://api.example/v1' } })
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-test' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.setExternalServer).toHaveBeenCalledWith({ base_url: 'https://api.example/v1', api_key: 'sk-test' }))
    expect(await screen.findByText('2 models available')).toBeInTheDocument()
    // The key is never shown back; it can be forgotten.
    expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('')
    fireEvent.click(screen.getByRole('button', { name: 'Forget key' }))
    await waitFor(() => expect(api.setExternalServer).toHaveBeenLastCalledWith({ base_url: 'https://api.example', clear_key: true }))
  })

  it("says why the models couldn't be read", async () => {
    vi.mocked(api.getExternalServer).mockResolvedValue({ base_url: 'https://api.example', has_key: true, models: [], error: 'the external server refused the API key (401)' })
    renderIt()
    expect(await screen.findByRole('alert')).toHaveTextContent('refused the API key')
  })
})
