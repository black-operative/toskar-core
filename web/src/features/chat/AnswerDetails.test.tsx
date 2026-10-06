import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AnswerDetails } from './AnswerDetails'
import { ChatErrorCard } from './ChatErrorCard'
import { downloadArtifact } from '@/lib/api'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  downloadArtifact: vi.fn(async () => undefined),
}))

describe('AnswerDetails', () => {
  it('saves a chat file named in the sources (#281)', () => {
    render(
      <AnswerDetails
        meta={{
          sources: [
            { kind: 'file', title: '10_Loaf_Pan_Recipes.pdf', source: 'Made in this chat', artifact_id: 'art-1' },
            { kind: 'file', title: 'old.pdf', source: 'Attached file' },
          ],
        }}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /10_Loaf_Pan_Recipes\.pdf/ }))
    expect(downloadArtifact).toHaveBeenCalledWith({ id: 'art-1', name: '10_Loaf_Pan_Recipes.pdf' })
    // A source recorded before files carried their ID stays a label.
    fireEvent.click(screen.getByRole('button', { name: /old\.pdf/ }))
    expect(downloadArtifact).toHaveBeenCalledTimes(1)
  })

  it('shows numbered sources and the steps on request', () => {
    render(
      <AnswerDetails
        meta={{
          sources: [
            { kind: 'web', title: 'Tire pressure, explained', url: 'https://www.example.com/psi' },
            { kind: 'knowledge', title: 'inventory.csv row 1', source: 'inventory.csv', snippet: 'sku: MP-1; price: 189.99' },
            { kind: 'knowledge', title: 'inventory.csv row 2', source: 'inventory.csv', snippet: 'sku: MP-2; price: 142.50' },
          ],
          steps: [
            { kind: 'search', text: 'Searched the web for “tire pressure”' },
            { kind: 'knowledge', text: 'Found 1 passage in inventory.csv' },
          ],
        }}
      />,
    )
    const link = screen.getByRole('link', { name: /Tire pressure, explained/ })
    expect(link).toHaveAttribute('href', 'https://www.example.com/psi')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText('example.com')).toBeInTheDocument()

    // Passages from one source share one chip.
    const chip = screen.getByRole('button', { name: /inventory\.csv/ })
    expect(chip).toHaveTextContent('· 2')
    fireEvent.click(chip)
    expect(screen.getByText('sku: MP-1; price: 189.99')).toBeInTheDocument()
    expect(screen.getByText('sku: MP-2; price: 142.50')).toBeInTheDocument()

    expect(screen.queryByText('Searched the web for “tire pressure”')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /What I did \(2 steps\)/ }))
    expect(screen.getByText('Searched the web for “tire pressure”')).toBeInTheDocument()
  })

  it('shows a notice when a fallback model answered', () => {
    render(
      <AnswerDetails
        meta={{
          steps: [{ kind: 'recover', text: 'Qwen 14B ran out of memory, so Llama 1B answered instead' }],
          notice: 'Qwen 14B could not answer, so the smaller Llama 1B answered instead. This answer may be less detailed.',
        }}
      />,
    )
    expect(screen.getByRole('note')).toHaveTextContent('may be less detailed')
    fireEvent.click(screen.getByRole('button', { name: /What I did \(1 step\)/ }))
    expect(screen.getByText(/ran out of memory/)).toBeInTheDocument()
  })

  it('lists files the assistant made, and only those', () => {
    render(
      <AnswerDetails
        meta={{
          files: [
            { id: 'a', name: 'Budget.xlsx', mime_type: 'x', kind: 'spreadsheet', size_bytes: 5120, producer: 'assistant' },
            { id: 'b', name: 'mine.csv', mime_type: 'x', kind: 'spreadsheet', size_bytes: 10, producer: 'user' },
          ],
        }}
      />,
    )
    expect(screen.getByText('Files')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Budget\.xlsx/ })).toHaveTextContent('Spreadsheet · 5 kB')
    expect(screen.queryByText('mine.csv')).not.toBeInTheDocument()
  })

  it('renders nothing for an answer that used nothing', () => {
    const { container } = render(<AnswerDetails meta={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('ChatErrorCard', () => {
  it('offers the next step and keeps technical detail behind Details', () => {
    const retry = vi.fn()
    const newChat = vi.fn()
    render(
      <MemoryRouter>
        <ChatErrorCard raw="the request exceeds the available context size" onRetry={retry} onNewChat={newChat} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('This conversation is too long for the model')
    expect(screen.queryByText('the request exceeds the available context size')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Start a new chat' }))
    expect(newChat).toHaveBeenCalled()
    expect(screen.getByRole('link', { name: 'Open Models' })).toHaveAttribute('href', '/models')
    fireEvent.click(screen.getByRole('button', { name: 'Details' }))
    expect(screen.getByText('the request exceeds the available context size')).toBeInTheDocument()
  })
})
