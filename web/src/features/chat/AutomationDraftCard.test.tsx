import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { Automation, AutomationDraft } from '@/types/api'
import { AutomationDraftCard, AutomationRunNote } from './AutomationDraftCard'

vi.mock('@/lib/api', () => ({
  api: { listAutomations: vi.fn(), createAutomation: vi.fn() },
}))

const draft: AutomationDraft = {
  id: 'draft-1',
  name: 'Morning news',
  prompt: 'Summarize the news.',
  schedule: { kind: 'daily', time_zone: 'America/Juneau', hour: 8, minute: 0 },
  notification: { mode: 'always' },
  profile_id: 'general-assistant',
  notes: [],
}

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>
        <AutomationDraftCard draft={draft} conversationId="conv-1" />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// A chat's draft is created only when the person presses Create, with the
// chat and the draft, so it keeps the conversation and isn't made twice (#204).
describe('AutomationDraftCard', () => {
  beforeEach(() => vi.mocked(api.listAutomations).mockResolvedValue([]))

  it('shows what it will do and creates it on confirmation', async () => {
    vi.mocked(api.createAutomation).mockResolvedValue({ id: 'auto-1', draft_id: 'draft-1' } as Automation)
    renderIt()
    expect(screen.getByText('Morning news')).toBeInTheDocument()
    expect(screen.getByText(/^Every day at 8:00\sAM$/)).toBeInTheDocument()
    expect(api.createAutomation).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Create automation' }))
    expect(await screen.findByRole('link', { name: 'Open in Automations' })).toHaveAttribute('href', '/automations?id=auto-1')
    expect(api.createAutomation).toHaveBeenCalledWith(
      expect.objectContaining({ model_id: 'auto', profile_id: 'general-assistant', conversation_id: 'conv-1', draft_id: 'draft-1', prompt: 'Summarize the news.' }),
    )
  })

  it('links to the automation a draft already made', async () => {
    vi.mocked(api.listAutomations).mockResolvedValue([{ id: 'auto-1', draft_id: 'draft-1' } as Automation])
    renderIt()
    expect(await screen.findByRole('link', { name: 'Open in Automations' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Create automation' })).not.toBeInTheDocument()
  })

  it('marks a result an automation posted to its chat', () => {
    render(
      <MemoryRouter>
        <AutomationRunNote run={{ automation_id: 'auto-1', run_id: 'r1', name: 'Morning news' }} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: 'Morning news' })).toHaveAttribute('href', '/automations?id=auto-1')
    expect(screen.getByText(/From the automation/)).toBeInTheDocument()
  })
})
