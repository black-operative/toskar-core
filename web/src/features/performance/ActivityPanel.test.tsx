import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { GenerationRun } from '@/types/api'
import { ActivityPanel } from './ActivityPanel'
import { RanOnTag } from './RanOnTag'

vi.mock('@/lib/api', () => ({ api: { getPerformance: vi.fn(), getModels: vi.fn() } }))

const run = (id: string, extra: Partial<GenerationRun>): GenerationRun =>
  ({
    id,
    conversation_title: `Chat ${id}`,
    model_id: 'qwen2.5-7b-q4',
    runtime_id: 'llamacpp',
    prompt_tokens: 10,
    completion_tokens: 20,
    total_tokens: 30,
    ttft_ms: 100,
    prompt_ms: 50,
    eval_ms: 400,
    total_ms: 500,
    prompt_tok_per_sec: 200,
    eval_tok_per_sec: 50,
    role_steps: [],
    cross_machine: false,
    node_count: 1,
    created_at: '2026-10-05T20:00:00Z',
    ...extra,
  }) as GenerationRun

describe('RanOnTag', () => {
  it('names the GPU with its device, the CPU, or nothing when not known', () => {
    const { rerender, container } = render(<RanOnTag backend="vulkan" device="AMD Radeon RX 7900 XTX" />)
    expect(screen.getByText('GPU').closest('span')).toHaveAttribute('title', 'Vulkan · AMD Radeon RX 7900 XTX')
    rerender(<RanOnTag backend="cpu" />)
    expect(screen.getByText('CPU')).toBeInTheDocument()
    rerender(<RanOnTag />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('ActivityPanel', () => {
  it('filters replies by GPU or CPU', async () => {
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.getPerformance).mockResolvedValue([
      run('a', { backend: 'vulkan', device: 'AMD Radeon RX 7900 XTX' }),
      run('b', { backend: 'cpu' }),
      run('c', {}),
    ])
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ActivityPanel />
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Chat a')).toBeInTheDocument()
    expect(screen.getByText('Chat c')).toBeInTheDocument()
    const filter = screen.getByRole('combobox', { name: 'Filter by GPU or CPU' })
    fireEvent.change(filter, { target: { value: 'gpu' } })
    expect(screen.getByText('Chat a')).toBeInTheDocument()
    expect(screen.queryByText('Chat b')).not.toBeInTheDocument()
    // A reply from before Toskar recorded the device matches neither.
    expect(screen.queryByText('Chat c')).not.toBeInTheDocument()
    fireEvent.change(filter, { target: { value: 'cpu' } })
    expect(screen.getByText('Chat b')).toBeInTheDocument()
    expect(screen.queryByText('Chat a')).not.toBeInTheDocument()
  })

  it('has no GPU or CPU filter when no reply recorded where it ran', async () => {
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.getPerformance).mockResolvedValue([run('c', {})])
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ActivityPanel />
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Chat c')).toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Filter by GPU or CPU' })).not.toBeInTheDocument()
  })
})
