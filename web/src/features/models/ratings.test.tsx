import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { ModelRating } from '@/types/api'
import { CommunityScore, RateButton, RatingDialogHost, RatingPrompt } from './ratings'

vi.mock('@/lib/api', () => ({
  ApiError: class extends Error {
    status = 0
  },
  api: {
    getModelRating: vi.fn(),
    putModelRating: vi.fn(),
    deleteModelRating: vi.fn(),
    dismissModelRating: vi.fn(),
    getCommunityRatings: vi.fn(),
  },
}))

const unrated: ModelRating = {
  model_id: 'qwen',
  rateable: true,
  tags: [],
  shared: false,
  share_observations: false,
  observations: { tokens_per_second: 41.5, ttft_ms: 320, starts: 3, start_failures: 1 },
  ask: true,
  shares: {
    destination: 'ratings.toskar.ai',
    model: { id: 'qwen2.5-coder-7b-instruct', format: 'gguf', quantization: 'Q4_K_M', runtime: 'llamacpp', backend: 'metal' },
    hardware: { platform: 'macos', architecture: 'arm64', vendor: 'apple', family: 'm4-max', memory_type: 'unified', memory_bucket_gb: '32-64' },
  },
}

function renderIt(ui: React.ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      {ui}
      <RatingDialogHost />
    </QueryClientProvider>,
  )
}

describe('ratings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getModelRating).mockResolvedValue(unrated)
    vi.mocked(api.putModelRating).mockResolvedValue({ ...unrated, stars: 4, tags: ['fast'], shared: true, ask: false })
    vi.mocked(api.dismissModelRating).mockResolvedValue(null)
  })

  it('lets the keyboard choose stars: one Tab stop, arrow keys move the rating', async () => {
    renderIt(<RateButton modelId="qwen" modelName="Qwen Coder" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Rate Qwen Coder' }))
    const stars = screen.getAllByRole('radio')
    expect(stars.map((s) => s.tabIndex)).toEqual([0, -1, -1, -1, -1])
    stars[0].focus()
    fireEvent.keyDown(stars[0], { key: 'ArrowRight' })
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' })
    const three = screen.getByRole('radio', { name: '3 stars' })
    expect(three).toHaveAttribute('aria-checked', 'true')
    expect(document.activeElement).toBe(three)
    expect(screen.getAllByRole('radio').map((s) => s.tabIndex)).toEqual([-1, -1, 0, -1, -1])
  })

  it('rates a model, showing exactly what sharing sends before it is shared', async () => {
    renderIt(<RateButton modelId="qwen" modelName="Qwen Coder" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Rate Qwen Coder' }))
    expect(screen.getByRole('dialog', { name: 'Rate Qwen Coder' })).toBeInTheDocument()
    const save = screen.getByRole('button', { name: 'Save' })
    expect(save).toBeDisabled()

    fireEvent.click(screen.getByRole('radio', { name: '4 stars' }))
    fireEvent.click(screen.getByRole('button', { name: 'Fast' }))
    fireEvent.click(screen.getByRole('checkbox', { name: /Share this rating/ }))
    expect(screen.getByText('apple m4-max · unified memory, 32-64 GB · macos arm64')).toBeInTheDocument()
    expect(screen.getByText('ratings.toskar.ai')).toBeInTheDocument()
    // A new rating is for the App language, and sharing says so.
    expect(screen.getByRole('radiogroup', { name: 'How well did it work for you in English?' })).toBeInTheDocument()
    expect(screen.getByText('Language').nextElementSibling).toHaveTextContent('English')

    // How it runs is a second choice, shown before it is made.
    const observe = screen.getByRole('checkbox', { name: /Include how it runs here/ })
    expect(observe).not.toBeChecked()
    expect(screen.getByText('41.5 tok/s, 320 ms to first token, 2 of 3 starts worked')).toBeInTheDocument()
    fireEvent.click(observe)

    fireEvent.click(screen.getByRole('button', { name: 'Save and share' }))
    await waitFor(() =>
      expect(api.putModelRating).toHaveBeenCalledWith('qwen', { stars: 4, tags: ['fast'], share: true, observations: true, language: 'en' }),
    )
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('keeps a rating private unless sharing is chosen', async () => {
    renderIt(<RateButton modelId="qwen" modelName="Qwen Coder" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Rate Qwen Coder' }))
    fireEvent.click(screen.getByRole('radio', { name: '2 stars' }))
    // How it runs can't be shared without the rating.
    expect(screen.getByRole('checkbox', { name: /Include how it runs here/ })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.putModelRating).toHaveBeenCalledWith('qwen', { stars: 2, tags: [], share: false, observations: false, language: 'en' }))
  })

  it('says which language a rating is for, or none', async () => {
    vi.mocked(api.putModelRating).mockResolvedValue({ ...unrated, stars: 5, language: 'es', ask: false })
    renderIt(<RateButton modelId="qwen" modelName="Qwen Coder" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Rate Qwen Coder' }))
    const language = screen.getByRole('combobox', { name: 'Language you used it in' })
    // One choice per language ratings collect by: Portuguese once, Chinese in each script.
    const options = Array.from((language as HTMLSelectElement).options).map((o) => o.value)
    expect(options.filter((v) => v === 'pt')).toHaveLength(1)
    expect(options).toEqual(expect.arrayContaining(['', 'es', 'zh-Hans', 'zh-Hant']))
    expect(options).not.toContain('pt-BR')

    fireEvent.change(language, { target: { value: 'es' } })
    fireEvent.click(screen.getByRole('radio', { name: '5 stars' }))
    expect(screen.getByText('How well did it work for you in Spanish?')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.putModelRating).toHaveBeenLastCalledWith('qwen', expect.objectContaining({ language: 'es' })))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    vi.mocked(api.putModelRating).mockClear()
    fireEvent.click(await screen.findByRole('button', { name: /Qwen Coder/ }))
    fireEvent.change(screen.getByRole('combobox', { name: 'Language you used it in' }), { target: { value: '' } })
    expect(screen.queryByText(/How well did it work for you/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio', { name: '3 stars' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.putModelRating).toHaveBeenCalledWith('qwen', { stars: 3, tags: [], share: false, observations: false }))
  })

  it('keeps the language a saved rating gives', async () => {
    vi.mocked(api.getModelRating).mockResolvedValue({ ...unrated, stars: 4, language: 'ja', ask: false })
    renderIt(<RateButton modelId="qwen" modelName="Qwen Coder" />)
    fireEvent.click(await screen.findByRole('button', { name: /Qwen Coder/ }))
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Language you used it in' })).toHaveValue('ja'))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  })

  it('asks after use, and stops when told', async () => {
    renderIt(<RatingPrompt modelId="qwen" modelName="Qwen Coder" />)
    expect(await screen.findByText('How is Qwen Coder working for you?')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Don’t ask again' }))
    await waitFor(() => expect(api.dismissModelRating).toHaveBeenCalledWith('qwen'))
    expect(screen.queryByText('How is Qwen Coder working for you?')).not.toBeInTheDocument()
  })

  it('shows similar-hardware and overall scores, labelling thin data', async () => {
    vi.mocked(api.getCommunityRatings).mockResolvedValue({
      enabled: true,
      models: {
        qwen: {
          similar: { tier: 'class', ratings: 4, average: 4.5, weighted_score: 3.9, confidence: 'early' },
          overall: { tier: 'global', ratings: 1200, average: 4.1, weighted_score: 4.1, confidence: 'community', observed: 300, median_tokens_per_second: 38, crash_rate: 0.02 },
        },
      },
    })
    renderIt(<CommunityScore modelId="qwen" />)
    expect(await screen.findByText('4 ratings')).toBeInTheDocument()
    expect(screen.getByText(/on similar computers/)).toBeInTheDocument()
    expect(screen.getByText('Early ratings')).toBeInTheDocument()
    expect(screen.getByText('1,200 ratings')).toBeInTheDocument()
    expect(screen.getAllByText('Early ratings')).toHaveLength(1)
    expect(screen.getByText('38.0 tok/s, 2% crashed')).toBeInTheDocument()
  })

  it('shows ratings by language, the App language first', async () => {
    vi.mocked(api.getCommunityRatings).mockResolvedValue({
      enabled: true,
      models: {
        qwen: {
          overall: { tier: 'global', ratings: 40, average: 4.1, weighted_score: 4.1, confidence: 'community' },
          languages: [
            { language: 'es', ratings: 30, average: 4.8, weighted_score: 4.7, confidence: 'community' },
            { language: 'de', ratings: 12, average: 4.2, weighted_score: 4.2, confidence: 'community' },
            { language: 'ja', ratings: 9, average: 4.1, weighted_score: 4.0, confidence: 'early' },
            { language: 'en', ratings: 5, average: 3, weighted_score: 3.2, confidence: 'early' },
          ],
        },
      },
    })
    const { container } = renderIt(<CommunityScore modelId="qwen" />)
    expect(await screen.findByText(/in English/)).toBeInTheDocument()
    expect(Array.from(container.querySelectorAll('p')).map((p) => p.textContent)).toEqual([
      '★ 4.1 overall · 40 ratings',
      '★ 3.2 in English · 5 ratingsEarly ratings',
      '★ 4.7 in Spanish · 30 ratings',
      '★ 4.2 in German · 12 ratings',
    ])
  })
})
