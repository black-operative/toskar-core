import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { WebhookLink } from './WebhookLink'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, makeAutomationHook: vi.fn() } }
})

// The link is shown once, when it's made, and a new one replaces it (#204).
describe('WebhookLink', () => {
  it('makes a link and shows it once', async () => {
    vi.mocked(api.makeAutomationHook).mockResolvedValue({ token: 'abc', path: '/hooks/abc' })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WebhookLink automationId="a1" hookSet />
      </QueryClientProvider>,
    )
    expect(screen.getByText(/shown only when it's made/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Make a new link' }))
    expect(await screen.findByDisplayValue(/\/hooks\/abc$/)).toBeInTheDocument()
    expect(screen.getByText(/shown only once/)).toBeInTheDocument()
    expect(api.makeAutomationHook).toHaveBeenCalledWith('a1')
  })
})
