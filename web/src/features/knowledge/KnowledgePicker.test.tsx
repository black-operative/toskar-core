import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { KnowledgeSource } from '@/types/api'
import { KnowledgePicker } from './KnowledgePicker'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, listKnowledge: vi.fn(), createKnowledge: vi.fn() } }
})

const src = (id: string, name: string): KnowledgeSource => ({
  id, name, kind: 'text', status: 'ready', chunk_count: 3, created_at: '', updated_at: '',
})

function renderPicker(selected: string[], onChange: (ids: string[]) => void) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>
        <KnowledgePicker selected={selected} onChange={onChange} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('KnowledgePicker', () => {
  it('attaches, removes, and connects a folder', async () => {
    vi.mocked(api.listKnowledge).mockResolvedValue([src('a', 'Inventory'), src('b', 'Policies')])
    vi.mocked(api.createKnowledge).mockResolvedValue(src('c', 'catalog'))
    const onChange = vi.fn()
    renderPicker(['a'], onChange)
    await waitFor(() => expect(screen.getByText('Inventory')).toBeInTheDocument())
    // The link to the Knowledge page keeps its word (a <link> tag in the string dropped it).
    expect(screen.getByRole('link', { name: 'Knowledge' })).toHaveAttribute('href', '/knowledge')

    fireEvent.change(screen.getByLabelText('Connect existing knowledge'), { target: { value: 'b' } })
    expect(onChange).toHaveBeenLastCalledWith(['a', 'b'])

    fireEvent.click(screen.getByRole('button', { name: 'Disconnect Inventory' }))
    expect(onChange).toHaveBeenLastCalledWith([])

    fireEvent.change(screen.getByLabelText('File or folder path'), { target: { value: '~/catalog' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith(['a', 'c']))
    expect(api.createKnowledge).toHaveBeenCalledWith({ kind: 'path', path: '~/catalog' })
  })
})
