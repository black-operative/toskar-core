import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { SpecializedAI } from '@/types/api'
import { TrainPage } from './TrainPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, listAIs: vi.fn(), getAI: vi.fn(), createExampleAI: vi.fn() } }
})

describe('TrainPage', () => {
  it('sets up the example AI from the empty state', async () => {
    const example = { id: 'ex-1', name: 'Tread Right Tires (example)', example: true } as SpecializedAI
    vi.mocked(api.listAIs).mockResolvedValue([])
    vi.mocked(api.getAI).mockResolvedValue(null)
    vi.mocked(api.createExampleAI).mockResolvedValue(example)
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={['/train']}>
          <TrainPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await waitFor(() => expect(screen.getByText('Nothing built yet.')).toBeInTheDocument())
    // One button: with nothing open, the header doesn't repeat the introduction's.
    const buttons = screen.getAllByRole('button', { name: 'Try an example' })
    expect(buttons).toHaveLength(1)
    expect(screen.getByRole('heading', { level: 1, name: 'Train' })).toBeInTheDocument()
    fireEvent.click(buttons[0])
    await waitFor(() => expect(api.createExampleAI).toHaveBeenCalledTimes(1))
  })
})
