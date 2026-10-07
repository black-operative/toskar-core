import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { api } from '@/lib/api'
import type { Automation, AutomationDetail } from '@/types/api'
import { AutomationsPage } from './AutomationsPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listAutomations: vi.fn(),
      getAutomation: vi.fn(),
      getProfiles: vi.fn(),
      listTools: vi.fn(),
      getModels: vi.fn(),
      createAutomation: vi.fn(),
      updateAutomation: vi.fn(),
      pauseAutomation: vi.fn(),
      resumeAutomation: vi.fn(),
      deleteAutomation: vi.fn(),
      runAutomation: vi.fn(),
      listAutomationRuns: vi.fn(),
      parseAutomation: vi.fn(),
      continueAutomationRun: vi.fn(),
    },
  }
})

const saved: Automation = {
  id: 'auto-1',
  name: 'Price below $500',
  enabled: true,
  schedule: { kind: 'daily', time_zone: 'UTC', hour: 8, minute: 0 },
  prompt: 'Check the price',
  profile_id: 'general-assistant',
  tools: [],
  notification: { mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500 } },
  created_at: '2026-09-28T15:00:00Z',
  updated_at: '2026-09-28T15:00:00Z',
  next_run_at: '2026-09-29T08:00:00Z',
  last_run_at: '2026-09-28T08:05:00Z',
  consecutive_failures: 0,
  last_status: 'succeeded',
  last_result: 'price is $420',
}

const detail: AutomationDetail = {
  ...saved,
  history: [
    {
      id: 'run-1',
      automation_id: 'auto-1',
      occurrence_at: '2026-09-28T08:00:00Z',
      status: 'succeeded',
      started_at: '2026-09-28T08:00:05Z',
      finished_at: '2026-09-28T08:05:00Z',
      result: 'price is $420',
      notification_sent: true,
      model_id: 'model-a',
      node_id: 'this-computer',
      attempt: 1,
    },
  ],
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AutomationsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AutomationsPage', () => {
  beforeEach(() => {
    vi.mocked(api.listAutomations).mockResolvedValue([saved])
    vi.mocked(api.getAutomation).mockResolvedValue(detail)
    vi.mocked(api.getProfiles).mockResolvedValue([
      { id: 'general-assistant', name: 'General', purpose: 'general', orchestrator_id: 'simple', roles: [], node_policy: { mode: 'automatic' } },
    ])
    vi.mocked(api.getModels).mockResolvedValue([
      {
        id: 'gemma-4-e4b',
        display_name: 'Gemma 4 E4B',
        capabilities: { tool_calling: true, vision: false, coding: true },
        installed: true,
      },
    ])
    vi.mocked(api.listTools).mockResolvedValue([
      {
        id: 'internet.search',
        name: 'Web Search',
        description: 'Search the public internet.',
        capability: 'internet',
        source: 'builtin',
        schema: '{}',
        default_policy: 'allow',
        risk: 'read',
        enabled: true,
        profiles: [],
      },
    ])
    vi.mocked(api.pauseAutomation).mockResolvedValue({ ...saved, enabled: false })
    vi.mocked(api.createAutomation).mockResolvedValue({ ...saved, id: 'auto-2' })
    // The computer reads requests (#204); its cases are in request_test.go.
    vi.mocked(api.parseAutomation).mockResolvedValue({
      name: 'Price below $500',
      prompt: 'Check this product. Report the current price.',
      schedule: { kind: 'daily', time_zone: 'America/Los_Angeles', hour: 8, minute: 0 },
      notification: { mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500, currency: 'USD' } },
      notes: [],
    })
  })

  it('continues a run in chat', async () => {
    vi.mocked(api.continueAutomationRun).mockResolvedValue({ conversation_id: 'conv-9' })
    function ChatSpy() {
      return <p>chat {new URLSearchParams(useLocation().search).get('c')}</p>
    }
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={['/automations']}>
          <Routes>
            <Route path="/automations" element={<AutomationsPage />} />
            <Route path="/chat" element={<ChatSpy />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: /Price below \$500/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Continue in chat' }))
    expect(await screen.findByText('chat conv-9')).toBeInTheDocument()
    expect(api.continueAutomationRun).toHaveBeenCalledWith('auto-1', 'run-1')
  })

  it('shows the latest result and runs the history actions', async () => {
    renderPage()
    expect(await screen.findByRole('button', { name: /Price below \$500/ })).toBeInTheDocument()
    expect(screen.getByText('price is $420')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Price below \$500/ }))
    expect(await screen.findByText('History')).toBeInTheDocument()
    expect(screen.getByText('Notified')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
    await waitFor(() => expect(api.pauseAutomation).toHaveBeenCalledWith('auto-1'))
  })

  it('loads older runs a page at a time', async () => {
    vi.mocked(api.getAutomation).mockResolvedValue({ ...detail, history_more: true })
    vi.mocked(api.listAutomationRuns).mockResolvedValue({
      runs: [{ ...detail.history[0], id: 'run-0', occurrence_at: '2026-09-27T08:00:00Z', result: 'price is $510', notification_sent: false }],
      more: false,
    })
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /Price below \$500/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Show older runs' }))
    await waitFor(() => expect(api.listAutomationRuns).toHaveBeenCalledWith('auto-1', 'run-1'))
    expect(await screen.findByText('price is $510')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show older runs' })).not.toBeInTheDocument()
  })

  it('explains automations and starts one from a template when there are none', async () => {
    vi.mocked(api.listAutomations).mockResolvedValue([])
    renderPage()
    expect(await screen.findByRole('heading', { name: /Put Toskar to work/ })).toBeInTheDocument()
    expect(screen.getByText('Say when')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Price watch/ }))
    // The template asks for its few fields, then fills in the details without reading a request.
    const fill = await screen.findByRole('button', { name: 'Fill in the details' })
    expect(fill).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Link'), { target: { value: 'shop.example.com/laptop' } })
    fireEvent.change(screen.getByLabelText('Notify below'), { target: { value: '1,299.99' } })
    fireEvent.click(fill)
    expect(await screen.findByDisplayValue('Price watch: shop.example.com')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Check the current price of the product at https://shop.example.com/laptop.')).toBeInTheDocument()
    expect(screen.getByText(/Notify when the price is below \$1,299\.99/, { selector: 'dd' })).toBeInTheDocument()
    expect(api.parseAutomation).not.toHaveBeenCalled()
  })

  it('describes a template in words instead, from its example', async () => {
    vi.mocked(api.listAutomations).mockResolvedValue([])
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /Folder summary/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Describe it in your own words instead' }))
    expect(screen.getByRole('textbox', { name: 'Describe what you want' })).toHaveValue(
      'Every day at 6:00 PM, summarize what is in this folder and notify me only when it changes.',
    )
  })

  it('shows how automations work on request when there are some', async () => {
    renderPage()
    const toggle = await screen.findByRole('button', { name: 'How it works' })
    expect(screen.queryByText('Say when')).not.toBeInTheDocument()
    fireEvent.click(toggle)
    expect(screen.getByText('Say when')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Hide how it works' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('saves a schedule on several days at several times', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'New automation' }))
    fireEvent.change(screen.getByRole('combobox', { name: 'Repeats' }), { target: { value: 'weekly' } })
    // Monday is on; add Wednesday and Friday, and an evening time.
    fireEvent.click(screen.getByRole('button', { name: 'Wednesday' }))
    fireEvent.click(screen.getByRole('button', { name: 'Friday' }))
    expect(screen.getByRole('button', { name: 'Friday' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: 'Add a time' }))
    const times = screen.getAllByLabelText('Time')
    fireEvent.change(times[1], { target: { value: '17:30' } })
    fireEvent.change(screen.getByRole('textbox', { name: /Name/ }), { target: { value: 'Standup notes' } })
    fireEvent.change(screen.getByRole('textbox', { name: /Task/ }), { target: { value: 'Summarize the team channel.' } })
    expect(await screen.findByRole('option', { name: 'Gemma 4 E4B' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create automation' }))
    await waitFor(() => expect(api.createAutomation).toHaveBeenCalled())
    const body = vi.mocked(api.createAutomation).mock.calls[0][0]
    expect(body.schedule).toMatchObject({ kind: 'weekly', weekdays: [1, 3, 5], times: [{ hour: 8, minute: 0 }, { hour: 17, minute: 30 }] })
  })

  it('fills a structured task from a description and saves it', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'New automation' }))
    fireEvent.change(screen.getByPlaceholderText(/Every morning at 8:00 AM/), {
      target: { value: 'Every morning at 8:00 AM, check this product and tell me if the price is below $500.' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Fill in the details' }))
    expect(await screen.findByDisplayValue('Price below $500')).toBeInTheDocument()
    expect(api.parseAutomation).toHaveBeenCalledWith(
      'Every morning at 8:00 AM, check this product and tell me if the price is below $500.',
      expect.any(String),
      'en',
    )
    expect(await screen.findByRole('option', { name: 'Gemma 4 E4B' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create automation' }))
    await waitFor(() => expect(api.createAutomation).toHaveBeenCalled())
    const body = vi.mocked(api.createAutomation).mock.calls[0][0]
    expect(body.schedule).toMatchObject({ kind: 'daily', hour: 8, minute: 0 })
    expect(body.notification).toEqual({ mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500, currency: 'USD' } })
    // The computer adds the price instruction when it runs; the page sends the task (#204).
    expect(body.prompt).not.toContain('{"price"')
    expect(body.profile_id).toBe('general-assistant')
    expect(body.model_id).toBe('gemma-4-e4b')
    // Results follow the assistant language unless the automation says otherwise (§22).
    expect(body.response_language).toBe('account')
  })

  it('saves the language results are written in', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'New automation' }))
    fireEvent.change(screen.getByPlaceholderText(/Every morning at 8:00 AM/), {
      target: { value: 'Every morning at 8:00 AM, check this product and tell me if the price is below $500.' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Fill in the details' }))
    expect(await screen.findByDisplayValue('Price below $500')).toBeInTheDocument()
    expect(await screen.findByRole('option', { name: 'Gemma 4 E4B' })).toBeInTheDocument()
    const language = screen.getByRole('combobox', { name: 'Results in' })
    expect([...language.querySelectorAll('option')].slice(0, 3).map((o) => o.textContent)).toEqual([
      'Same as the assistant language',
      'Same as the App language',
      'The language of the request',
    ])
    fireEvent.change(language, { target: { value: 'de' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create automation' }))
    await waitFor(() => expect(api.createAutomation).toHaveBeenCalled())
    expect(vi.mocked(api.createAutomation).mock.calls[0][0].response_language).toBe('de')
  })

  it('lists tools that change things apart, offers Auto, and saves approvals', async () => {
    vi.mocked(api.listTools).mockResolvedValue([
      ...((await api.listTools()) ?? []),
      {
        id: 'files.write',
        name: 'Write files',
        description: 'Create or change files.',
        capability: 'files',
        source: 'builtin',
        schema: '{}',
        default_policy: 'ask',
        risk: 'write',
        enabled: true,
        profiles: [],
      },
    ])
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'New automation' }))
    expect(await screen.findByText('These change things on this computer')).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Auto/ })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Only when it fails' })).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText(/Every morning at 8:00 AM/), {
      target: { value: 'Every morning at 8:00 AM, check this product and tell me if the price is below $500.' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Fill in the details' }))
    expect(await screen.findByDisplayValue('Price below $500')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: /Write files/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Create automation' }))
    await waitFor(() => expect(api.createAutomation).toHaveBeenCalled())
    expect(vi.mocked(api.createAutomation).mock.calls.at(-1)?.[0].tools).toContain('files.write')
  })
})
