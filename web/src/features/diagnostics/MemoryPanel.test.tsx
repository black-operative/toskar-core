import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { RuntimeSample } from '@/types/api'
import { growingFor, MemoryPanel } from './MemoryPanel'

const series = (heaps: number[], goroutines = 40): RuntimeSample[] =>
  heaps.map((heap, i) => ({ at: new Date(i * 300_000).toISOString(), goroutines, heap_bytes: heap, sys_bytes: heap * 2 }))

describe('growingFor', () => {
  const mb = 1024 * 1024
  it('flags memory that climbs steadily for hours', () => {
    // 48 samples five minutes apart: four hours, from 50 MB up to about 97 MB.
    expect(growingFor(series(Array.from({ length: 48 }, (_, i) => (50 + i) * mb)), 300)).toBe(3)
  })
  it('ignores a short history, a flat line, and a single spike', () => {
    expect(growingFor(series(Array.from({ length: 12 }, (_, i) => (50 + i * 10) * mb)), 300)).toBe(0)
    expect(growingFor(series(Array.from({ length: 48 }, () => 50 * mb)), 300)).toBe(0)
    const spike = Array.from({ length: 48 }, (_, i) => (i % 2 === 0 ? 50 : 49) * mb)
    spike[47] = 120 * mb
    expect(growingFor(series(spike), 300)).toBe(0)
  })
})


vi.mock('@/lib/api', () => ({ api: { getRuntimeHistory: vi.fn() } }))

function renderPanel() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryPanel />
    </QueryClientProvider>,
  )
}

describe('MemoryPanel', () => {
  it('shows current memory and background tasks', async () => {
    vi.mocked(api.getRuntimeHistory).mockResolvedValue({
      started_at: '2026-10-03T08:00:00Z',
      interval_seconds: 300,
      now: { at: '2026-10-03T09:00:00Z', goroutines: 42, heap_bytes: 30 * 1024 * 1024, sys_bytes: 60 * 1024 * 1024 },
      samples: [],
    })
    renderPanel()
    expect(await screen.findByText("Toskar's memory")).toBeInTheDocument()
    expect(screen.getByText('42')).toBeInTheDocument()
  })

  // An older daemon, or anything else unexpected, must not break the page.
  it('renders nothing for a response without counts', async () => {
    vi.mocked(api.getRuntimeHistory).mockResolvedValue([] as never)
    const { container } = renderPanel()
    await waitFor(() => expect(api.getRuntimeHistory).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
  })
})
